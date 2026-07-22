#!/bin/sh
set -eu

# This wrapper intentionally treats an absent tool, token, or running stack as
# a documented skip. The k6 profile itself remains strict once execution starts.

SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd "$SCRIPT_DIR/../.." && pwd)
BASE_URL=${BASE_URL:-http://localhost:8080}
BASE_URL=${BASE_URL%/}
export BASE_URL

skip() {
  printf 'SKIP Academy k6: %s\n' "$*" >&2
  exit 0
}

command -v k6 >/dev/null 2>&1 || skip "k6 не установлен"
command -v curl >/dev/null 2>&1 || skip "curl не установлен; readiness проверить нельзя"
[ -n "${PUBLIC_ACADEMY_TOKEN:-}" ] || skip "задайте PUBLIC_ACADEMY_TOKEN через окружение"

if ! curl -fsS --connect-timeout 2 --max-time 5 "$BASE_URL/readyz" >/dev/null 2>&1; then
  skip "gateway или его зависимости не готовы по $BASE_URL/readyz"
fi

exec k6 run "$REPO_ROOT/tests/k6/academy-public-campaign.js"
