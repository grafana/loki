curl -X POST \
  -H "Content-Type: application/json" \
  -d '{ "query": "{service_name=\"getting-started-gateway-1\"}", "from": 0, "to": 5709269800000000000}' \
  http://localhost:3102/loki/api/v1/patterns | jq .
