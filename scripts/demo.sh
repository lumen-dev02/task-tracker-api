#!/usr/bin/env bash
# Requires a running API, curl, jq and openssl. Leaves demo users/tasks in the DB.
set -euo pipefail

for tool in curl jq openssl; do
  command -v "$tool" >/dev/null || { echo "Required tool not found: $tool" >&2; exit 1; }
done

BASE_URL="${BASE_URL:-http://127.0.0.1:8081}"
BASE_URL="${BASE_URL%/}"
suffix="$(openssl rand -hex 8)"
password="$(openssl rand -hex 24)"
alice="alice-${suffix}@example.com"
bob="bob-${suffix}@example.com"

register() {
  jq -n --arg email "$1" --arg password "$password" '{email:$email,name:"Demo user",password:$password}' |
    curl --fail-with-body -sS "$BASE_URL/api/v1/auth/register" -H 'Content-Type: application/json' --data-binary @- >/dev/null
}

login() {
  jq -n --arg email "$1" --arg password "$password" '{email:$email,password:$password}' |
    curl --fail-with-body -sS "$BASE_URL/api/v1/auth/login" -H 'Content-Type: application/json' --data-binary @- |
    jq -er '.access_token'
}

curl --fail-with-body -sS "$BASE_URL/readyz" >/dev/null
register "$alice"
register "$bob"
alice_token="$(login "$alice")"
bob_token="$(login "$bob")"

task="$(curl --fail-with-body -sS "$BASE_URL/api/v1/tasks" \
  -H "Authorization: Bearer $alice_token" -H 'Content-Type: application/json' \
  -d '{"title":"Write API tests","description":"Check owner permissions"}')"
id="$(jq -er '.id' <<<"$task")"
echo "Created task $id"

code="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/api/v1/tasks/$id" -H "Authorization: Bearer $bob_token")"
if [[ "$code" != "404" ]]; then
  echo "Expected 404 for another user, got $code" >&2
  exit 1
fi
echo "Another user cannot read the task: 404"

curl --fail-with-body -sS -X PUT "$BASE_URL/api/v1/tasks/$id" \
  -H "Authorization: Bearer $alice_token" -H 'Content-Type: application/json' \
  -d '{"title":"Write API tests","description":"Check owner permissions","status":"done"}' |
  jq -e '.status == "done"' >/dev/null

curl --fail-with-body -sS "$BASE_URL/api/v1/tasks?status=done&limit=20&offset=0" \
  -H "Authorization: Bearer $alice_token" | jq .
echo "Demo completed. Credentials and tokens were not printed."
