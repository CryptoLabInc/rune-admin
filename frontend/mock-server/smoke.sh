#!/usr/bin/env bash
# Smoke test for the Rune Console mock server. Exercises the auth flow, the
# session-expiry trigger, and one endpoint per domain.
# Usage: BASE=http://localhost:4000 bash mock-server/smoke.sh
set -euo pipefail
BASE="${BASE:-http://localhost:4000}"
pass=0; fail=0

# check <label> <expected-status> <curl-args...>
check() {
  local label="$1" want="$2"; shift 2
  local got
  got=$(curl -s -o /dev/null -w "%{http_code}" "$@")
  if [[ "$got" == "$want" ]]; then
    printf "  \033[32mok\033[0m   %-42s %s\n" "$label" "$got"; pass=$((pass+1))
  else
    printf "  \033[31mFAIL\033[0m %-42s want %s got %s\n" "$label" "$want" "$got"; fail=$((fail+1))
  fi
}

echo "== reset =="
curl -s -X POST "$BASE/__mock/reset" >/dev/null

echo "== auth & session =="
check "GET  /console/session (seeded login)" 200 "$BASE/console/session"
check "POST /console/auth/start"             200 -X POST "$BASE/console/auth/start"
# Full redirect flow: start -> extract authorize_url -> follow to callback.
STATE=$(curl -s -X POST "$BASE/console/auth/start" | sed -E 's/.*state=([^"]+)".*/\1/')
check "GET  /mock-authorize"                 200 "$BASE/mock-authorize?state=$STATE"
check "GET  /auth/callback (bad state->302)" 302 "$BASE/auth/callback?code=x&state=bogus"

echo "== session-expiry trigger =="
curl -s -X POST "$BASE/__mock/session/expire" >/dev/null
check "GET  /api/v1/workspace after expire"  401 "$BASE/api/v1/workspace"
check "GET  /console/session after expire"   200 "$BASE/console/session"
curl -s -X POST "$BASE/__mock/session/login" >/dev/null
check "GET  /api/v1/workspace after re-login" 200 "$BASE/api/v1/workspace"

echo "== workspace =="
check "GET  /api/v1/workspace"               200 "$BASE/api/v1/workspace"
check "POST /api/v1/workspace/stop"          202 -X POST "$BASE/api/v1/workspace/stop"

echo "== workspace failure injection =="
curl -s -X POST "$BASE/__mock/reset" >/dev/null
check "POST /__mock/workspace/fail?op=get"   200 -X POST "$BASE/__mock/workspace/fail?op=get"
check "GET  /api/v1/workspace (injected 502)" 502 "$BASE/api/v1/workspace"
check "GET  /api/v1/workspace (recovered)"   200 "$BASE/api/v1/workspace"
check "POST /__mock/workspace/fail?op=stop"  200 -X POST "$BASE/__mock/workspace/fail?op=stop"
check "POST /workspace/stop (injected 502)"  502 -X POST "$BASE/api/v1/workspace/stop"
check "POST /__mock/workspace/fail bad op"   400 -X POST "$BASE/__mock/workspace/fail?op=bogus"

echo "== workspace orphan flag =="
check "POST /__mock/workspace/orphan"        200 -X POST "$BASE/__mock/workspace/orphan"
curl -s -X POST "$BASE/__mock/reset" >/dev/null

echo "== teams =="
check "GET  /api/v1/teams/tree"              200 "$BASE/api/v1/teams/tree"
check "GET  /api/v1/teams/t_1"               200 "$BASE/api/v1/teams/t_1"
check "GET  /api/v1/teams/nope (404)"        404 "$BASE/api/v1/teams/nope"
check "POST /api/v1/teams"                   201 -X POST -H 'Content-Type: application/json' \
      -d '{"name":"QA","parentId":"t_1"}' "$BASE/api/v1/teams"

echo "== team members =="
check "GET  /api/v1/teams/t_1/members"       200 "$BASE/api/v1/teams/t_1/members?page=1&size=10"
check "GET  members size>100 (400)"          400 "$BASE/api/v1/teams/t_1/members?size=101"

echo "== users =="
check "GET  /api/v1/users"                   200 "$BASE/api/v1/users?page=1&size=10"
check "GET  /api/v1/users?status=online"     200 "$BASE/api/v1/users?status=online"
check "GET  /api/v1/users/u_1"               200 "$BASE/api/v1/users/u_1"
check "GET  /api/v1/users/stats"             200 "$BASE/api/v1/users/stats"
check "DEL  /api/v1/users (no ids -> 400)"   400 -X DELETE "$BASE/api/v1/users"

echo "== memberships =="
check "POST /api/v1/users/u_2/members/roles" 201 -X POST -H 'Content-Type: application/json' \
      -d '{"teamId":"t_4","role":"read"}' "$BASE/api/v1/users/u_2/members/roles"

echo "== invitations =="
check "POST /api/v1/invitations"             201 -X POST -H 'Content-Type: application/json' \
      -d '{"account":"fresh@corp.com","username":"김신입","memberships":[{"teamId":"t_1","role":"read"}]}' "$BASE/api/v1/invitations"
check "POST /api/v1/invitations/resend"      200 -X POST -H 'Content-Type: application/json' \
      -d '{"userId":"u_3"}' "$BASE/api/v1/invitations/resend"
check "GET  /api/v1/invitations?view=history" 200 "$BASE/api/v1/invitations?view=history"

echo
echo "== batch partial-failure shape =="
curl -s -X DELETE "$BASE/api/v1/users?userIds=u_5,does_not_exist" | tr ',' '\n' | grep -E 'succeeded|failed|code' || true

echo
printf "passed=%d failed=%d\n" "$pass" "$fail"
[[ "$fail" == 0 ]] || exit 1
