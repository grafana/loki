service=getting-started-gateway-1

curl -X POST http://localhost:3102/loki/api/v1/patterns \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "query={service=\"${service}\"}" \
  -d 'from=2020-09-14T15:22:25.479Z' \
  -d 'to=2025-09-14T15:23:25.479Z' \
  | jq .
