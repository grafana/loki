#!/usr/bin/env bash
set -euo pipefail

LOKI_URL="http://loki:3100"
PASSED=0
FAILED=0
TOTAL=0

pass() { TOTAL=$((TOTAL + 1)); echo "  PASS [$TOTAL]: $1"; PASSED=$((PASSED + 1)); }
fail() { TOTAL=$((TOTAL + 1)); echo "  FAIL [$TOTAL]: $1"; FAILED=$((FAILED + 1)); }

TENANT_A="tenant-a"
TENANT_B="tenant-b"

# --------------------------------------------------------------------------
# Test 1: Health check
# --------------------------------------------------------------------------
echo ""
echo "=== Test 1: Health Check ==="
READY=false
for i in $(seq 1 30); do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${LOKI_URL}/ready" 2>/dev/null || true)
  if [ "$STATUS" = "200" ]; then
    READY=true
    break
  fi
  sleep 2
done

if $READY; then
  pass "Loki is ready"
else
  fail "Loki did not become ready within 60s"
  echo "RESULTS: $PASSED passed, $FAILED failed out of $TOTAL"
  exit 1
fi

# Wait for ingester ring to stabilize
sleep 5

# --------------------------------------------------------------------------
# Test 2: Log ingestion (tenant-a)
# --------------------------------------------------------------------------
echo ""
echo "=== Test 2: Log Ingestion ==="
TIMESTAMP=$(date +%s)000000000
PUSH_BODY=$(cat <<EOF
{
  "streams": [
    {
      "stream": { "job": "e2e-test", "env": "docker" },
      "values": [
        ["${TIMESTAMP}", "Hello from e2e test - tenant-a"]
      ]
    }
  ]
}
EOF
)

HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "${LOKI_URL}/loki/api/v1/push" \
  -H "Content-Type: application/json" \
  -H "X-Scope-OrgID: ${TENANT_A}" \
  -d "$PUSH_BODY")

if [ "$HTTP_CODE" = "204" ]; then
  pass "Push to ${TENANT_A} returned HTTP 204"
else
  fail "Push to ${TENANT_A} returned HTTP $HTTP_CODE (expected 204)"
fi

# Also push to tenant-b
PUSH_BODY_B=$(cat <<EOF
{
  "streams": [
    {
      "stream": { "job": "e2e-test-b", "env": "docker" },
      "values": [
        ["${TIMESTAMP}", "Hello from e2e test - tenant-b"]
      ]
    }
  ]
}
EOF
)

HTTP_CODE_B=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "${LOKI_URL}/loki/api/v1/push" \
  -H "Content-Type: application/json" \
  -H "X-Scope-OrgID: ${TENANT_B}" \
  -d "$PUSH_BODY_B")

if [ "$HTTP_CODE_B" = "204" ]; then
  pass "Push to ${TENANT_B} returned HTTP 204"
else
  fail "Push to ${TENANT_B} returned HTTP $HTTP_CODE_B (expected 204)"
fi

sleep 3

# --------------------------------------------------------------------------
# Test 3: Log query -- verify pushed data appears for correct tenant
# --------------------------------------------------------------------------
echo ""
echo "=== Test 3: Log Query ==="

QUERY_A=$(curl -s -G "${LOKI_URL}/loki/api/v1/query_range" \
  --data-urlencode 'query={job="e2e-test"}' \
  --data-urlencode "start=$(( $(date +%s) - 300 ))000000000" \
  --data-urlencode "end=$(( $(date +%s) + 60 ))000000000" \
  --data-urlencode "limit=10" \
  -H "X-Scope-OrgID: ${TENANT_A}" 2>/dev/null)

STATUS_A=$(echo "$QUERY_A" | jq -r '.status' 2>/dev/null || echo "error")
COUNT_A=$(echo "$QUERY_A" | jq '.data.result | length' 2>/dev/null || echo "0")

if [ "$STATUS_A" = "success" ] && [ "$COUNT_A" -gt 0 ] 2>/dev/null; then
  pass "tenant-a query returned $COUNT_A stream(s)"
else
  fail "tenant-a query: status=$STATUS_A count=$COUNT_A (expected success, >0)"
fi

# --------------------------------------------------------------------------
# Test 4: Tenant isolation -- tenant-a cannot see tenant-b data
# --------------------------------------------------------------------------
echo ""
echo "=== Test 4: Tenant Data Isolation ==="

QUERY_CROSS=$(curl -s -G "${LOKI_URL}/loki/api/v1/query_range" \
  --data-urlencode 'query={job="e2e-test-b"}' \
  --data-urlencode "start=$(( $(date +%s) - 300 ))000000000" \
  --data-urlencode "end=$(( $(date +%s) + 60 ))000000000" \
  --data-urlencode "limit=10" \
  -H "X-Scope-OrgID: ${TENANT_A}" 2>/dev/null)

CROSS_COUNT=$(echo "$QUERY_CROSS" | jq '.data.result | length' 2>/dev/null || echo "0")

if [ "$CROSS_COUNT" = "0" ] 2>/dev/null; then
  pass "tenant-a cannot see tenant-b data (0 results for job=e2e-test-b)"
else
  fail "tenant-a can see tenant-b data ($CROSS_COUNT results -- isolation broken)"
fi

# --------------------------------------------------------------------------
# Test 5: Rate limiting -- /loki/api/v1/labels returns 200, then 429
# --------------------------------------------------------------------------
echo ""
echo "=== Test 5: Rate Limiting (request count) ==="

# Use a fresh tenant so previous requests don't exhaust the bucket
RATE_TENANT="ratelimit"
SUCCESS_COUNT=0
RATE_LIMITED=false

for i in $(seq 1 15); do
  RESP=$(curl -s -o /dev/null -w "%{http_code}" \
    "${LOKI_URL}/loki/api/v1/labels" \
    -H "X-Scope-OrgID: ${RATE_TENANT}")

  echo "    Request $i: HTTP $RESP"

  if [ "$RESP" = "200" ]; then
    SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
  elif [ "$RESP" = "429" ]; then
    RATE_LIMITED=true
    break
  else
    echo "    (unexpected status)"
  fi
done

if $RATE_LIMITED && [ "$SUCCESS_COUNT" -gt 0 ]; then
  pass "Got $SUCCESS_COUNT successful requests then HTTP 429 (limit=10)"
elif $RATE_LIMITED && [ "$SUCCESS_COUNT" -eq 0 ]; then
  fail "Got 429 but no successful (200) requests before it -- rate limiter may be miscounting"
else
  fail "Never got 429 after 15 requests ($SUCCESS_COUNT returned 200)"
fi

# --------------------------------------------------------------------------
# Test 6: Rate limit headers on 429 response
# --------------------------------------------------------------------------
echo ""
echo "=== Test 6: Rate Limit Headers ==="

HEADER_RESP=$(curl -s -D - -o /dev/null \
  "${LOKI_URL}/loki/api/v1/labels" \
  -H "X-Scope-OrgID: ${RATE_TENANT}" 2>/dev/null)

HTTP_LINE=$(echo "$HEADER_RESP" | head -1)
HAS_RETRY=$(echo "$HEADER_RESP" | grep -ci "Retry-After" || true)
HAS_LIMIT=$(echo "$HEADER_RESP" | grep -ci "X-RateLimit" || true)

if echo "$HTTP_LINE" | grep -q "429"; then
  pass "Still rate limited (429) on subsequent request"
else
  fail "Expected 429, got: $(echo "$HTTP_LINE" | tr -d '\r')"
fi

if [ "$HAS_RETRY" -gt 0 ]; then
  RETRY_VAL=$(echo "$HEADER_RESP" | grep -i "Retry-After" | head -1 | tr -d '\r')
  pass "Retry-After header present: $RETRY_VAL"
else
  fail "Missing Retry-After header"
fi

if [ "$HAS_LIMIT" -gt 0 ]; then
  LIMIT_VAL=$(echo "$HEADER_RESP" | grep -i "X-RateLimit" | head -1 | tr -d '\r')
  pass "X-RateLimit header present: $LIMIT_VAL"
else
  echo "  INFO: X-RateLimit header not set (SDK uses Retry-After only -- this is expected)"
fi

# --------------------------------------------------------------------------
# Test 7: Multi-tenant rate limit isolation
# --------------------------------------------------------------------------
echo ""
echo "=== Test 7: Multi-Tenant Rate Limit Isolation ==="

# tenant "ratelimit" is exhausted. A different tenant should NOT be limited.
OTHER_TENANT="ratelimit-other"

OTHER_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  "${LOKI_URL}/loki/api/v1/labels" \
  -H "X-Scope-OrgID: ${OTHER_TENANT}")

if [ "$OTHER_CODE" = "200" ]; then
  pass "Different tenant '${OTHER_TENANT}' is NOT rate limited (HTTP 200)"
else
  fail "Different tenant got HTTP $OTHER_CODE (expected 200 -- rate limits should be per-tenant)"
fi

# Also verify original tenant is still blocked
ORIG_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  "${LOKI_URL}/loki/api/v1/labels" \
  -H "X-Scope-OrgID: ${RATE_TENANT}")

if [ "$ORIG_CODE" = "429" ]; then
  pass "Original tenant '${RATE_TENANT}' is still rate limited (HTTP 429)"
else
  fail "Original tenant got HTTP $ORIG_CODE (expected 429 -- should still be blocked)"
fi

# --------------------------------------------------------------------------
# Test 8: No tenant header is rejected (auth_enabled: true)
# --------------------------------------------------------------------------
echo ""
echo "=== Test 8: Missing Tenant Header Rejected ==="

NO_TENANT_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  "${LOKI_URL}/loki/api/v1/labels" 2>/dev/null)

if [ "$NO_TENANT_CODE" = "401" ] || [ "$NO_TENANT_CODE" = "422" ]; then
  pass "Request without X-Scope-OrgID rejected with HTTP $NO_TENANT_CODE"
else
  fail "Request without tenant header returned HTTP $NO_TENANT_CODE (expected 401 or 422)"
fi

# --------------------------------------------------------------------------
# Summary
# --------------------------------------------------------------------------
echo ""
echo "========================================"
echo "  RESULTS: $PASSED passed, $FAILED failed (out of $TOTAL tests)"
echo "========================================"
echo ""

if [ "$FAILED" -gt 0 ]; then
  exit 1
fi
exit 0
