#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

echo "Building Loki from source and starting e2e test environment..."
echo ""

docker compose up --build --abort-on-container-exit --exit-code-from test-runner
EXIT_CODE=$?

echo ""
echo "Cleaning up..."
docker compose down -v

exit $EXIT_CODE
