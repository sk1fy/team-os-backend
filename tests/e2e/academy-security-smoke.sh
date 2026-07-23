#!/bin/sh
set -eu

# Optional runtime-only secrets: PUBLIC_ACADEMY_TOKEN, ACCESS_TOKEN, or the
# one-shot ACADEMY_CHALLENGE_ID + ACADEMY_VERIFICATION_CODE pair. Rate-limit
# verification is enabled by default and may throttle this client IP briefly;
# set ACADEMY_CHECK_RATE_LIMIT=0 to skip that single check.

BASE_URL=${BASE_URL:-http://localhost:8080}
BASE_URL=${BASE_URL%/}
PUBLIC_APP_ORIGIN=${PUBLIC_APP_ORIGIN:-http://localhost:5173}
ACADEMY_CHECK_RATE_LIMIT=${ACADEMY_CHECK_RATE_LIMIT:-1}
RATE_LIMIT_ATTEMPTS=${RATE_LIMIT_ATTEMPTS:-40}
INVALID_TOKEN=academy-security-invalid-token-0000000000000000
LEGACY_COURSE_ID=00000000-0000-0000-0000-000000000001
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/teamos-academy-security.XXXXXX")
HEADERS_FILE=$TMP_DIR/headers
BODY_FILE=$TMP_DIR/body
HTTP_CODE=
REQ_ORIGIN=
REQ_COOKIE=
REQ_AUTH=
REQ_BODY=
REQ_IDEMPOTENCY=
REQ_PREFLIGHT_HEADERS=

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

fail() {
  printf 'Ошибка Academy security smoke: %s\n' "$*" >&2
  exit 1
}

skip() {
  printf 'SKIP Academy security smoke: %s\n' "$*" >&2
  exit 0
}

reset_request() {
  REQ_ORIGIN=
  REQ_COOKIE=
  REQ_AUTH=
  REQ_BODY=
  REQ_IDEMPOTENCY=
  REQ_PREFLIGHT_HEADERS=
}

request() {
  method=$1
  path=$2
  : >"$HEADERS_FILE"
  : >"$BODY_FILE"
  set -- -sS --connect-timeout 3 --max-time 10 -D "$HEADERS_FILE" -o "$BODY_FILE" -w '%{http_code}' -X "$method"
  [ -z "$REQ_ORIGIN" ] || set -- "$@" -H "Origin: $REQ_ORIGIN"
  [ -z "$REQ_COOKIE" ] || set -- "$@" -H "Cookie: $REQ_COOKIE"
  [ -z "$REQ_AUTH" ] || set -- "$@" -H "Authorization: Bearer $REQ_AUTH"
  [ -z "$REQ_IDEMPOTENCY" ] || set -- "$@" -H "Idempotency-Key: $REQ_IDEMPOTENCY"
  if [ -n "$REQ_PREFLIGHT_HEADERS" ]; then
    set -- "$@" -H 'Access-Control-Request-Method: POST' -H "Access-Control-Request-Headers: $REQ_PREFLIGHT_HEADERS"
  fi
  if [ -n "$REQ_BODY" ]; then
    set -- "$@" -H 'Content-Type: application/json' --data "$REQ_BODY"
  fi
  HTTP_CODE=$(curl "$@" "$BASE_URL$path") || fail "$method request не выполнен"
}

header_contains() {
  name=$1
  fragment=$2
  awk -v name="$name" -v fragment="$fragment" '
    index(tolower($0), tolower(name) ":") == 1 && index(tolower($0), tolower(fragment)) > 0 { found = 1 }
    END { exit found ? 0 : 1 }
  ' "$HEADERS_FILE"
}

assert_public_headers() {
  header_contains 'Cache-Control' 'private' || fail 'отсутствует Cache-Control: private'
  header_contains 'Cache-Control' 'no-store' || fail 'отсутствует Cache-Control: no-store'
  header_contains 'X-Robots-Tag' 'noindex' || fail 'отсутствует X-Robots-Tag: noindex'
  header_contains 'X-Robots-Tag' 'nofollow' || fail 'отсутствует X-Robots-Tag: nofollow'
}

assert_cookie_flags() {
  cookie_line=$1
  printf '%s\n' "$cookie_line" | grep -Eqi '(^|;[[:space:]]*)Path=/api/v1/public/academy([;]|$)' || fail 'cookie имеет слишком широкий Path'
  printf '%s\n' "$cookie_line" | grep -Eqi '(^|;[[:space:]]*)HttpOnly([;]|$)' || fail 'cookie не содержит HttpOnly'
  printf '%s\n' "$cookie_line" | grep -Eqi '(^|;[[:space:]]*)SameSite=Lax([;]|$)' || fail 'cookie не содержит SameSite=Lax'
  if [ "$EXPECT_SECURE_COOKIE" = 1 ]; then
    printf '%s\n' "$cookie_line" | grep -Eqi '(^|;[[:space:]]*)Secure([;]|$)' || fail 'cookie не содержит Secure'
  fi
  if printf '%s\n' "$cookie_line" | grep -Eqi '(^|;[[:space:]]*)Domain='; then
    fail 'cookie не должна задавать широкий Domain'
  fi
}

command -v curl >/dev/null 2>&1 || skip 'curl не установлен'

case "$RATE_LIMIT_ATTEMPTS" in
  ''|*[!0-9]*) fail 'RATE_LIMIT_ATTEMPTS должен быть целым числом' ;;
esac
[ "$RATE_LIMIT_ATTEMPTS" -gt 0 ] || fail 'RATE_LIMIT_ATTEMPTS должен быть больше нуля'
case "$ACADEMY_CHECK_RATE_LIMIT" in
  0|1) ;;
  *) fail 'ACADEMY_CHECK_RATE_LIMIT должен быть 0 или 1' ;;
esac

EXPECT_SECURE_COOKIE=${EXPECT_SECURE_COOKIE:-auto}
if [ "$EXPECT_SECURE_COOKIE" = auto ]; then
  case "$BASE_URL" in
    https://*) EXPECT_SECURE_COOKIE=1 ;;
    *) EXPECT_SECURE_COOKIE=0 ;;
  esac
fi
case "$EXPECT_SECURE_COOKIE" in
  0|1) ;;
  *) fail 'EXPECT_SECURE_COOKIE должен быть auto, 0 или 1' ;;
esac

if ! curl -fsS --connect-timeout 2 --max-time 5 "$BASE_URL/readyz" >/dev/null 2>&1; then
  skip "gateway или его зависимости не готовы по $BASE_URL/readyz"
fi

printf '1/6 Заголовки и visitor-cookie публичной кампании\n'
LANDING_TOKEN=${PUBLIC_ACADEMY_TOKEN:-$INVALID_TOKEN}
reset_request
request GET "/api/v1/public/academy/access/$LANDING_TOKEN"
if [ -n "${PUBLIC_ACADEMY_TOKEN:-}" ]; then
  [ "$HTTP_CODE" = 200 ] || fail "валидный campaign landing вернул HTTP $HTTP_CODE"
else
  [ "$HTTP_CODE" = 404 ] || fail "неизвестный access token должен возвращать HTTP 404, получен $HTTP_CODE"
fi
assert_public_headers
VISITOR_COOKIE=$(grep -i '^Set-Cookie: teamos_academy_visitor=' "$HEADERS_FILE" | head -n 1 | tr -d '\r' || true)
[ -n "$VISITOR_COOKIE" ] || fail 'visitor-cookie не установлена'
assert_cookie_flags "$VISITOR_COOKIE"

printf '2/6 CORS preflight для идемпотентных mutation endpoints\n'
reset_request
REQ_ORIGIN=$PUBLIC_APP_ORIGIN
REQ_PREFLIGHT_HEADERS='content-type,idempotency-key'
request OPTIONS "/api/v1/public/academy/access/$INVALID_TOKEN/activate"
case "$HTTP_CODE" in
  200|204) ;;
  *) fail "CORS preflight вернул HTTP $HTTP_CODE" ;;
esac
header_contains 'Access-Control-Allow-Origin' "$PUBLIC_APP_ORIGIN" || fail 'CORS не разрешил PUBLIC_APP_ORIGIN'
header_contains 'Access-Control-Allow-Headers' 'idempotency-key' || fail 'CORS не разрешил обязательный Idempotency-Key'

printf '3/6 CSRF отклоняет отсутствующий и чужой Origin\n'
for origin in missing foreign; do
  reset_request
  REQ_COOKIE='teamos_academy_external=invalid-session'
  REQ_IDEMPOTENCY="security-smoke-$origin"
  [ "$origin" = missing ] || REQ_ORIGIN='https://attacker.invalid'
  request POST "/api/v1/public/academy/access/$INVALID_TOKEN/activate"
  [ "$HTTP_CODE" = 403 ] || fail "CSRF $origin Origin должен вернуть HTTP 403, получен $HTTP_CODE"
  assert_public_headers
done

printf '4/6 Legacy public course остаётся read-only\n'
for method in POST PUT PATCH DELETE; do
  reset_request
  REQ_AUTH=${ACCESS_TOKEN:-}
  request "$method" "/api/v1/public/academy/courses/$LEGACY_COURSE_ID"
  case "$HTTP_CODE" in
    401|404|405) ;;
    *) fail "legacy public $method неожиданно вернул HTTP $HTTP_CODE" ;;
  esac
done

printf '5/6 Опциональная проверка external session-cookie\n'
if [ -n "${ACADEMY_CHALLENGE_ID:-}" ] || [ -n "${ACADEMY_VERIFICATION_CODE:-}" ]; then
  [ -n "${ACADEMY_CHALLENGE_ID:-}" ] && [ -n "${ACADEMY_VERIFICATION_CODE:-}" ] || \
    fail 'ACADEMY_CHALLENGE_ID и ACADEMY_VERIFICATION_CODE задаются только вместе'
  case "$ACADEMY_VERIFICATION_CODE" in
    [0-9][0-9][0-9][0-9][0-9][0-9]) ;;
    *) fail 'ACADEMY_VERIFICATION_CODE должен состоять из шести цифр' ;;
  esac
  reset_request
  REQ_BODY=$(printf '{"code":"%s"}' "$ACADEMY_VERIFICATION_CODE")
  request POST "/api/v1/public/academy/verifications/$ACADEMY_CHALLENGE_ID/confirm"
  [ "$HTTP_CODE" = 200 ] || fail "подтверждение подготовленного challenge вернуло HTTP $HTTP_CODE"
  assert_public_headers
  SESSION_COOKIE=$(grep -i '^Set-Cookie: teamos_academy_external=' "$HEADERS_FILE" | head -n 1 | tr -d '\r' || true)
  [ -n "$SESSION_COOKIE" ] || fail 'external session-cookie не установлена'
  assert_cookie_flags "$SESSION_COOKIE"
else
  printf '  SKIP session-cookie: передайте одноразовые ACADEMY_CHALLENGE_ID и ACADEMY_VERIFICATION_CODE\n'
fi

printf '6/6 Rate limit публичных mutation endpoints\n'
if [ "$ACADEMY_CHECK_RATE_LIMIT" = 1 ]; then
  attempt=1
  limited=0
  while [ "$attempt" -le "$RATE_LIMIT_ATTEMPTS" ]; do
    reset_request
    REQ_BODY='{"email":"security-smoke@invalid.example"}'
    request POST "/api/v1/public/academy/access/$INVALID_TOKEN/request-verification"
    if [ "$HTTP_CODE" = 429 ]; then
      limited=1
      break
    fi
    [ "$HTTP_CODE" = 404 ] || fail "до rate limit ожидался HTTP 404, получен $HTTP_CODE"
    attempt=$((attempt + 1))
  done
  [ "$limited" = 1 ] || fail "rate limit не сработал за $RATE_LIMIT_ATTEMPTS запросов"
  header_contains 'Retry-After' '' || fail 'HTTP 429 не содержит Retry-After'
else
  printf '  SKIP rate limit: ACADEMY_CHECK_RATE_LIMIT=0\n'
fi

printf 'Academy security smoke успешно завершён; токены и PII не выводились.\n'
