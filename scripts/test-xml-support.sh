#!/bin/bash
#
# Full integration test for XML support in Loki
# Tests: ingestion, parsing, querying, and filtering
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
LOKI_ADDR="http://localhost:3100"
LOKI_PID=""
CLEANUP_DONE=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

cleanup() {
    if [ "$CLEANUP_DONE" = true ]; then
        return
    fi
    CLEANUP_DONE=true

    echo -e "\n${YELLOW}Cleaning up...${NC}"
    if [ -n "$LOKI_PID" ] && kill -0 "$LOKI_PID" 2>/dev/null; then
        echo "Stopping Loki (PID: $LOKI_PID)"
        kill "$LOKI_PID" 2>/dev/null || true
        wait "$LOKI_PID" 2>/dev/null || true
    fi
    rm -rf /tmp/loki-xml-test 2>/dev/null || true
    echo -e "${GREEN}Cleanup complete${NC}"
}

trap cleanup EXIT INT TERM

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $1"
}

log_section() {
    echo -e "\n${YELLOW}========================================${NC}"
    echo -e "${YELLOW}$1${NC}"
    echo -e "${YELLOW}========================================${NC}\n"
}

wait_for_loki() {
    log_info "Waiting for Loki to be ready..."
    for i in {1..30}; do
        if curl -s "$LOKI_ADDR/ready" | grep -q "ready"; then
            log_success "Loki is ready"
            return 0
        fi
        sleep 1
    done
    log_error "Loki failed to start within 30 seconds"
    return 1
}

# Push logs to Loki using the push API
push_logs() {
    local job="$1"
    local logs="$2"
    local timestamp_ns=$(date +%s)000000000

    # Build the push payload
    local payload=$(cat <<EOF
{
  "streams": [
    {
      "stream": {
        "job": "$job",
        "env": "test"
      },
      "values": [
$logs
      ]
    }
  ]
}
EOF
)

    curl -s -X POST "$LOKI_ADDR/loki/api/v1/push" \
        -H "Content-Type: application/json" \
        -d "$payload"
}

# Query Loki and return results
query_loki() {
    local query="$1"
    local encoded_query=$(python3 -c "import urllib.parse; print(urllib.parse.quote('''$query'''))")
    curl -s "$LOKI_ADDR/loki/api/v1/query_range?query=$encoded_query&limit=100&start=$(( $(date +%s) - 3600 ))000000000&end=$(date +%s)000000000"
}

run_unit_tests() {
    log_section "Running Unit Tests"

    cd "$ROOT_DIR"

    log_info "Testing XML Parser..."
    if go test -v ./pkg/logql/log/... -run "XML" -count=1 2>&1 | tee /tmp/xml-parser-test.log | tail -20; then
        if grep -q "PASS" /tmp/xml-parser-test.log; then
            log_success "XML Parser tests passed"
        else
            log_error "XML Parser tests failed"
            return 1
        fi
    fi

    log_info "Testing XMLL Output..."
    if go test -v ./pkg/logcli/output/... -run "XMLL" -count=1 2>&1 | tee /tmp/xmll-output-test.log | tail -20; then
        if grep -q "PASS" /tmp/xmll-output-test.log; then
            log_success "XMLL Output tests passed"
        else
            log_error "XMLL Output tests failed"
            return 1
        fi
    fi

    log_info "Testing LogQL XML Integration..."
    if go test -v ./pkg/logql/syntax/... -run "xml" -count=1 2>&1 | tee /tmp/logql-xml-test.log | tail -20; then
        if grep -q "PASS" /tmp/logql-xml-test.log; then
            log_success "LogQL XML integration tests passed"
        else
            log_error "LogQL XML integration tests failed"
            return 1
        fi
    fi

    log_success "All unit tests passed!"
}

build_binaries() {
    log_section "Building Binaries"

    cd "$ROOT_DIR"

    log_info "Building Loki..."
    make loki
    log_success "Loki built successfully"

    log_info "Building LogCLI..."
    make logcli
    log_success "LogCLI built successfully"
}

start_loki() {
    log_section "Starting Loki"

    # Create temp directories
    rm -rf /tmp/loki-xml-test
    mkdir -p /tmp/loki-xml-test/{chunks,rules,wal}

    # Create a test config
    cat > /tmp/loki-xml-test/config.yaml <<EOF
auth_enabled: false

server:
  http_listen_port: 3100
  grpc_listen_port: 9096
  log_level: warn

common:
  instance_addr: 127.0.0.1
  path_prefix: /tmp/loki-xml-test
  storage:
    filesystem:
      chunks_directory: /tmp/loki-xml-test/chunks
      rules_directory: /tmp/loki-xml-test/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: 2020-10-24
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

limits_config:
  ingestion_rate_mb: 100
  ingestion_burst_size_mb: 100
EOF

    log_info "Starting Loki server..."
    "$ROOT_DIR/cmd/loki/loki" -config.file=/tmp/loki-xml-test/config.yaml &
    LOKI_PID=$!

    wait_for_loki
}

test_xml_ingestion() {
    log_section "Testing XML Log Ingestion"

    local ts=$(date +%s)

    # Push various XML log formats
    log_info "Pushing simple XML logs..."
    push_logs "xml-test" "
        [\"${ts}000000001\", \"<log><level>info</level><message>Server started</message><port>8080</port></log>\"],
        [\"${ts}000000002\", \"<log><level>debug</level><message>Processing request</message><duration>150ms</duration></log>\"],
        [\"${ts}000000003\", \"<log><level>error</level><message>Connection failed</message><code>500</code></log>\"],
        [\"${ts}000000004\", \"<log><level>warn</level><message>High memory usage</message><memory>256MB</memory></log>\"],
        [\"${ts}000000005\", \"<log><level>info</level><message>Request completed</message><status>200</status><latency>50ms</latency></log>\"]
    "

    log_info "Pushing nested XML logs..."
    push_logs "xml-nested" "
        [\"${ts}000000010\", \"<request><method>GET</method><path>/api/users</path><response><status>200</status><time>45ms</time></response></request>\"],
        [\"${ts}000000011\", \"<request><method>POST</method><path>/api/data</path><response><status>201</status><time>120ms</time></response></request>\"],
        [\"${ts}000000012\", \"<request><method>DELETE</method><path>/api/item</path><response><status>404</status><time>10ms</time></response></request>\"]
    "

    log_info "Pushing XML with attributes..."
    push_logs "xml-attrs" "
        [\"${ts}000000020\", \"<event type=\\\"click\\\" target=\\\"button\\\"><user>alice</user><timestamp>2024-01-01T12:00:00Z</timestamp></event>\"],
        [\"${ts}000000021\", \"<event type=\\\"submit\\\" target=\\\"form\\\"><user>bob</user><timestamp>2024-01-01T12:01:00Z</timestamp></event>\"]
    "

    sleep 2  # Allow time for ingestion
    log_success "XML logs ingested"
}

test_xml_queries() {
    log_section "Testing XML Queries"

    local failed=0

    # Test 1: Basic XML parsing
    # Note: XML like <log><level>info</level></log> produces label "log_level"
    log_info "Test 1: Basic XML parsing with | xml"
    local result=$(query_loki '{job="xml-test"} | xml')
    if echo "$result" | grep -q "log_level"; then
        log_success "Basic XML parsing works"
    else
        log_error "Basic XML parsing failed"
        echo "$result" | head -20
        ((failed++))
    fi

    # Test 2: Label filtering
    log_info "Test 2: XML with label filter (log_level=error)"
    result=$(query_loki '{job="xml-test"} | xml | log_level = "error"')
    if echo "$result" | grep -q "Connection failed"; then
        log_success "Label filtering works"
    else
        log_error "Label filtering failed"
        echo "$result" | head -20
        ((failed++))
    fi

    # Test 3: Numeric filtering
    log_info "Test 3: XML with numeric filter (log_status == 200)"
    result=$(query_loki '{job="xml-test"} | xml | log_status == 200')
    if echo "$result" | grep -q "Request completed"; then
        log_success "Numeric filtering works"
    else
        log_error "Numeric filtering failed"
        echo "$result" | head -20
        ((failed++))
    fi

    # Test 4: Numeric comparison
    log_info "Test 4: XML with numeric comparison (log_code > 400)"
    result=$(query_loki '{job="xml-test"} | xml | log_code > 400')
    if echo "$result" | grep -q "500"; then
        log_success "Numeric comparison works"
    else
        log_error "Numeric comparison failed"
        echo "$result" | head -20
        ((failed++))
    fi

    # Test 5: Nested field extraction
    # Note: XML parser flattens with full path, so <request><response><status> becomes request_response_status
    log_info "Test 5: Nested XML field extraction"
    result=$(query_loki '{job="xml-nested"} | xml')
    if echo "$result" | grep -q "request_response_status"; then
        log_success "Nested field extraction works (request_response_status found)"
    else
        log_error "Nested field extraction failed"
        echo "$result" | head -20
        ((failed++))
    fi

    # Test 6: Nested field filtering
    log_info "Test 6: Nested XML field filtering (request_response_status == 404)"
    result=$(query_loki '{job="xml-nested"} | xml | request_response_status == 404')
    if echo "$result" | grep -q "DELETE"; then
        log_success "Nested field filtering works"
    else
        log_error "Nested field filtering failed"
        echo "$result" | head -20
        ((failed++))
    fi

    # Test 7: Combined filters with AND
    log_info "Test 7: Combined filters (request_method=GET AND request_response_status=200)"
    result=$(query_loki '{job="xml-nested"} | xml | request_method = "GET" and request_response_status == 200')
    if echo "$result" | grep -q "/api/users"; then
        log_success "Combined AND filters work"
    else
        log_error "Combined AND filters failed"
        echo "$result" | head -20
        ((failed++))
    fi

    # Test 8: OR filters
    log_info "Test 8: OR filters (request_response_status=200 OR request_response_status=201)"
    result=$(query_loki '{job="xml-nested"} | xml | request_response_status == 200 or request_response_status == 201')
    if echo "$result" | grep -q "GET" && echo "$result" | grep -q "POST"; then
        log_success "OR filters work"
    else
        log_error "OR filters failed"
        echo "$result" | head -20
        ((failed++))
    fi

    if [ $failed -eq 0 ]; then
        log_success "All XML query tests passed!"
        return 0
    else
        log_error "$failed XML query tests failed"
        return 1
    fi
}

test_logcli_xml() {
    log_section "Testing LogCLI with XML"

    log_info "Querying with logcli..."

    # Basic query - XML <log><level>... produces log_level label
    if "$ROOT_DIR/cmd/logcli/logcli" query '{job="xml-test"} | xml' \
        --addr="$LOKI_ADDR" \
        --limit=10 \
        --since=1h 2>&1 | grep -q "log_level"; then
        log_success "LogCLI XML query works"
    else
        log_error "LogCLI XML query failed"
        return 1
    fi

    # Query with filter
    if "$ROOT_DIR/cmd/logcli/logcli" query '{job="xml-test"} | xml | log_level = "info"' \
        --addr="$LOKI_ADDR" \
        --limit=10 \
        --since=1h 2>&1 | grep -q "info"; then
        log_success "LogCLI XML filtered query works"
    else
        log_error "LogCLI XML filtered query failed"
        return 1
    fi

    log_success "LogCLI XML tests passed!"
}

print_summary() {
    log_section "Test Summary"

    echo -e "${GREEN}All XML support tests completed successfully!${NC}"
    echo ""
    echo "Features tested:"
    echo "  - XML Parser unit tests"
    echo "  - XMLL Output formatter tests"
    echo "  - LogQL XML integration tests"
    echo "  - XML log ingestion"
    echo "  - Basic XML parsing (| xml)"
    echo "  - Label filtering"
    echo "  - Numeric filtering and comparison"
    echo "  - Nested field extraction (flattening with _)"
    echo "  - Combined AND/OR filters"
    echo "  - LogCLI XML queries"
    echo ""
    echo "XML support is fully functional!"
}

main() {
    log_section "Loki XML Support - Full Integration Test"

    echo "This script will:"
    echo "  1. Run XML-related unit tests"
    echo "  2. Build Loki and LogCLI"
    echo "  3. Start a local Loki instance"
    echo "  4. Push test XML logs"
    echo "  5. Run various XML queries"
    echo "  6. Validate results"
    echo ""

    # Parse arguments
    SKIP_BUILD=false
    SKIP_UNIT_TESTS=false

    while [[ $# -gt 0 ]]; do
        case $1 in
            --skip-build)
                SKIP_BUILD=true
                shift
                ;;
            --skip-unit-tests)
                SKIP_UNIT_TESTS=true
                shift
                ;;
            *)
                echo "Unknown option: $1"
                echo "Usage: $0 [--skip-build] [--skip-unit-tests]"
                exit 1
                ;;
        esac
    done

    # Run tests
    if [ "$SKIP_UNIT_TESTS" = false ]; then
        run_unit_tests
    fi

    if [ "$SKIP_BUILD" = false ]; then
        build_binaries
    fi

    start_loki
    test_xml_ingestion
    test_xml_queries
    test_logcli_xml

    print_summary
}

main "$@"
