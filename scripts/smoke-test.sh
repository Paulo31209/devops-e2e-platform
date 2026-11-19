#!/usr/bin/env bash
# Smoke test against a running Notes API.
#   ./scripts/smoke-test.sh http://localhost:8080
set -Eeuo pipefail

BASE_URL="${1:-http://localhost:8080}"
pass=0

check() {
  local name="$1"; shift
  if "$@"; then
    echo "PASS  $name"
    pass=$((pass + 1))
  else
    echo "FAIL  $name" >&2
    exit 1
  fi
}

status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

check "GET /healthz returns 200"   test "$(status "$BASE_URL/healthz")" = 200
check "GET /readyz returns 200"    test "$(status "$BASE_URL/readyz")" = 200

created="$(curl -fs -X POST "$BASE_URL/api/v1/notes" \
  -H 'Content-Type: application/json' \
  -d '{"title":"smoke test","body":"created by CI"}')"
id="$(sed -E 's/.*"id":([0-9]+).*/\1/' <<<"$created")"
check "POST /api/v1/notes creates a note" test -n "$id"

check "GET /api/v1/notes/$id returns 200"    test "$(status "$BASE_URL/api/v1/notes/$id")" = 200
check "POST with empty title returns 422"     test "$(status -X POST "$BASE_URL/api/v1/notes" -d '{"title":""}')" = 422
check "DELETE /api/v1/notes/$id returns 204"  test "$(status -X DELETE "$BASE_URL/api/v1/notes/$id")" = 204
check "GET deleted note returns 404"          test "$(status "$BASE_URL/api/v1/notes/$id")" = 404
check "/metrics exposes http_requests_total"  grep -q '^http_requests_total' <(curl -fs "$BASE_URL/metrics")

echo "All $pass checks passed against $BASE_URL"
