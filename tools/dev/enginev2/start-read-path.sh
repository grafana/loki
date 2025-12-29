#!/bin/bash
set -e

# Helper script to start the Loki read path with Engine V2

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CONFIG_FILE="$REPO_ROOT/tools/dev/enginev2/loki-read-config.yaml"
DATA_DIR="$REPO_ROOT/tools/dev/enginev2/data/loki"

echo "🚀 Starting Loki Query Engine V2 (Read Path)"
echo "============================================="
echo ""

# Check if Loki binary exists
if [ ! -f "$REPO_ROOT/cmd/loki/loki" ]; then
    echo "❌ Loki binary not found at $REPO_ROOT/cmd/loki/loki"
    echo ""
    echo "Please build Loki first:"
    echo "  cd $REPO_ROOT"
    echo "  make loki"
    exit 1
fi

# Check if data directory exists
if [ ! -d "$DATA_DIR" ]; then
    echo "⚠️  Data directory not found: $DATA_DIR"
    echo ""
    echo "Make sure the write path is running:"
    echo "  cd $REPO_ROOT/tools/dev/enginev2"
    echo "  docker-compose up -d"
    echo ""
    echo "Then push some logs and wait 60 seconds for flush."
    exit 1
fi

# Check if DataObj files exist
DATAOBJ_COUNT=$(find "$DATA_DIR/dataobj" -name "*.dataobj" 2>/dev/null | wc -l)
if [ "$DATAOBJ_COUNT" -eq 0 ]; then
    echo "⚠️  No DataObj files found in $DATA_DIR/dataobj"
    echo ""
    echo "Push some logs and wait for flush:"
    echo "  curl -X POST http://localhost:3100/loki/api/v1/push \\"
    echo "    -H 'Content-Type: application/json' \\"
    echo "    -d '{...}'"
    echo ""
    echo "  sleep 60  # Wait for flush"
    echo ""
fi

echo "📁 Data directory: $DATA_DIR"
echo "📄 Config file: $CONFIG_FILE"
echo "🗃️  DataObj files found: $DATAOBJ_COUNT"
echo ""
echo "⚙️  Configuration:"
echo "  - HTTP Port: 3200"
echo "  - gRPC Port: 9096"
echo "  - Engine V2: enabled"
echo "  - Storage Lag: 1h"
echo ""
echo "Starting querier..."
echo ""

# Start Loki querier
exec "$REPO_ROOT/cmd/loki/loki" \
    -target=querier \
    -config.file="$CONFIG_FILE"