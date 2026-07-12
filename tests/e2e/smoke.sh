#!/bin/sh
set -eu

BASE_URL=${BASE_URL:-http://localhost:8080}
BASE_URL=${BASE_URL%/}
E2E_PASSWORD=${E2E_PASSWORD:-TeamOS-e2e-123}
E2E_TIMEOUT=${E2E_TIMEOUT:-30}
RUN_ID="$(date +%s)-$$"
OWNER_EMAIL=${OWNER_EMAIL:-"owner-${RUN_ID}@e2e.test"}
MEMBER_EMAIL=${MEMBER_EMAIL:-"member-${RUN_ID}@e2e.test"}
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/teamos-e2e.XXXXXX")
RESPONSE=
SSE_PID=

cleanup() {
  if [ -n "$SSE_PID" ]; then
    kill "$SSE_PID" 2>/dev/null || true
    wait "$SSE_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

fail() {
  printf 'Ошибка e2e: %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "не найдена команда $1"
}

request() {
  method=$1
  path=$2
  expected=$3
  token=${4:-}
  data=${5:-}
  body="$TMP_DIR/response.json"
  if [ -n "$token" ] && [ -n "$data" ]; then
    code=$(curl -sS -o "$body" -w '%{http_code}' -X "$method" \
      -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
      --data "$data" "$BASE_URL$path")
  elif [ -n "$token" ]; then
    code=$(curl -sS -o "$body" -w '%{http_code}' -X "$method" \
      -H "Authorization: Bearer $token" "$BASE_URL$path")
  elif [ -n "$data" ]; then
    code=$(curl -sS -o "$body" -w '%{http_code}' -X "$method" \
      -H 'Content-Type: application/json' --data "$data" "$BASE_URL$path")
  else
    code=$(curl -sS -o "$body" -w '%{http_code}' -X "$method" "$BASE_URL$path")
  fi
  RESPONSE=$(cat "$body")
  [ "$code" = "$expected" ] || fail "$method $path: ожидался HTTP $expected, получен $code: $RESPONSE"
}

json_required() {
  value=$(printf '%s' "$RESPONSE" | jq -er "$1") || fail "в ответе отсутствует $2: $RESPONSE"
  printf '%s' "$value"
}

wait_for_lesson_update() {
  attempt=0
  while [ "$attempt" -lt "$E2E_TIMEOUT" ]; do
    request GET "/api/v1/academy/lessons?courseId=$COURSE_ID" 200 "$OWNER_TOKEN"
    updated=$(printf '%s' "$RESPONSE" | jq -r --arg id "$ARTICLE_ID" --arg title "$UPDATED_TITLE" \
      'any(.[]; .sourceArticleId == $id and .sourceMode == "link" and .title == $title)')
    [ "$updated" = true ] && return 0
    attempt=$((attempt + 1))
    sleep 1
  done
  fail "link-урок не обновился после изменения статьи за ${E2E_TIMEOUT} секунд"
}

wait_for_notification() {
  attempt=0
  while [ "$attempt" -lt "$E2E_TIMEOUT" ]; do
    request GET /api/v1/notifications 200 "$MEMBER_TOKEN"
    found=$(printf '%s' "$RESPONSE" | jq -r --arg title "$ARTICLE_TITLE" \
      'any(.[]; .type == "article_published" and (.title | contains($title)))')
    [ "$found" = true ] && return 0
    attempt=$((attempt + 1))
    sleep 1
  done
  fail "уведомление о статье не появилось за ${E2E_TIMEOUT} секунд"
}

need curl
need jq

printf '1/9 Регистрация компании и владельца\n'
request POST /api/v1/auth/register 201 '' "$(jq -nc \
  --arg company "E2E Компания $RUN_ID" --arg email "$OWNER_EMAIL" --arg password "$E2E_PASSWORD" \
  '{companyName:$company,email:$email,password:$password,firstName:"E2E",lastName:"Владелец"}')"
OWNER_TOKEN=$(json_required '.accessToken' accessToken)

printf '2/9 Приглашение и принятие приглашения\n'
request POST /api/v1/org/invites 201 "$OWNER_TOKEN" "$(jq -nc --arg email "$MEMBER_EMAIL" \
  '{email:$email,role:"employee"}')"
INVITE_TOKEN=$(json_required '.token' token)
request GET "/api/v1/auth/invites/$INVITE_TOKEN" 200
request POST "/api/v1/auth/invites/$INVITE_TOKEN/accept" 200 '' "$(jq -nc --arg password "$E2E_PASSWORD" \
  '{firstName:"E2E",lastName:"Сотрудник",password:$password}')"
MEMBER_TOKEN=$(json_required '.accessToken' accessToken)

printf '3/9 Подключение SSE сотрудника\n'
curl -sSN --max-time "$((E2E_TIMEOUT + 10))" -H "Authorization: Bearer $MEMBER_TOKEN" \
  "$BASE_URL/api/v1/notifications/stream" >"$TMP_DIR/events.sse" 2>"$TMP_DIR/events.err" &
SSE_PID=$!
sleep 1

printf '4/9 Создание раздела и публикация статьи\n'
request POST /api/v1/kb/sections 201 "$OWNER_TOKEN" "$(jq -nc --arg name "E2E Раздел $RUN_ID" \
  '{name:$name,parentId:null,access:{scope:"company",departmentIds:[],positionIds:[],userIds:[]}}')"
SECTION_ID=$(json_required '.id' id)
ARTICLE_TITLE="E2E Статья $RUN_ID"
CONTENT=$(jq -nc '{type:"doc",content:[{type:"paragraph",content:[{type:"text",text:"Первая версия"}]}]}')
request POST /api/v1/kb/articles 201 "$OWNER_TOKEN" "$(jq -nc --arg section "$SECTION_ID" --arg title "$ARTICLE_TITLE" \
  --argjson content "$CONTENT" '{sectionId:$section,title:$title,content:$content,status:"published",requiresAcknowledgement:true}')"
ARTICLE_ID=$(json_required '.id' id)

printf '5/9 Получение уведомления через REST и SSE\n'
wait_for_notification
attempt=0
while [ "$attempt" -lt "$E2E_TIMEOUT" ]; do
  grep -q '^event: notification' "$TMP_DIR/events.sse" 2>/dev/null && break
  attempt=$((attempt + 1))
  sleep 1
done
grep -q '^event: notification' "$TMP_DIR/events.sse" 2>/dev/null || \
  fail "SSE не доставил событие notification за ${E2E_TIMEOUT} секунд"

printf '6/9 Ознакомление сотрудника со статьёй\n'
request POST "/api/v1/kb/articles/$ARTICLE_ID/acknowledge" 204 "$MEMBER_TOKEN"
request GET "/api/v1/kb/articles/$ARTICLE_ID/acknowledgements" 200 "$OWNER_TOKEN"
printf '%s' "$RESPONSE" | jq -e 'length > 0' >/dev/null || fail "ознакомление со статьёй не сохранено"

printf '7/9 Создание link-курса из базы знаний\n'
request POST /api/v1/academy/courses/from-kb 201 "$OWNER_TOKEN" "$(jq -nc \
  --arg title "E2E Курс $RUN_ID" --arg section "$SECTION_ID" --arg article "$ARTICLE_ID" \
  '{title:$title,mode:"link",sectionIds:[$section],articleIds:[$article],sequential:true}')"
COURSE_ID=$(json_required '.id' id)
request GET "/api/v1/academy/lessons?courseId=$COURSE_ID" 200 "$OWNER_TOKEN"
printf '%s' "$RESPONSE" | jq -e --arg id "$ARTICLE_ID" \
  'any(.[]; .sourceArticleId == $id and .sourceMode == "link")' >/dev/null || fail "link-урок не создан"

printf '8/9 Изменение статьи и проверка репликации в урок\n'
UPDATED_TITLE="$ARTICLE_TITLE — обновлено"
UPDATED_CONTENT=$(jq -nc '{type:"doc",content:[{type:"paragraph",content:[{type:"text",text:"Вторая версия"}]}]}')
request PATCH "/api/v1/kb/articles/$ARTICLE_ID" 200 "$OWNER_TOKEN" "$(jq -nc \
  --arg title "$UPDATED_TITLE" --argjson content "$UPDATED_CONTENT" '{title:$title,content:$content}')"
wait_for_lesson_update

printf '9/9 Проверка health/readiness gateway\n'
request GET /healthz 200
request GET /readyz 200

printf 'E2E smoke успешно завершён: register → invite → accept → publish → ack → link-course → sync → SSE.\n'
