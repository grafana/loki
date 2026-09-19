#!/bin/bash

# Usage: ./run_tests.sh [options]
# Options:
#   -t, --test PATTERN     Test to run (default: all tests)
#                          Examples: Txn, Group, Txn/range, Group/sticky
#   -n, --iterations NUM   Max iterations (default: 50)
#   -r, --records NUM      Number of records (default: 500000)
#   --race                 Enable race detector (default: off)
#   -l, --log-level LEVEL  Set log level for both client and server
#   --client-log LEVEL     Set KGO_LOG_LEVEL for the test client only
#   --server-log LEVEL     Set kfake server log level only
#   -v, --version VERSION  Kafka version to emulate (e.g., 2.8, 3.5)
#   --pprof ADDR           Enable pprof on server (e.g., :6060)
#   --data-dir DIR         Persistence directory for kfake server
#   --restart SECS         Kill and restart server after SECS seconds (requires --data-dir)
#   --timeout DURATION     Test timeout (default: 180s, 450s with --race)
#   --keep-logs            Keep per-iteration logs (client_N.log, server_N.log)
#   -k, --kill             Kill processes on ports 9092-9094 and exit
#   --clean                Kill servers and remove /tmp/kfake_test_logs
#   -h, --help             Show this help

MAX_ITERATIONS=50
RECORDS=500000
TEST_TYPE=""
RACE=""
CLIENT_LOG=""
SERVER_LOG_LEVEL=""
KFAKE_VERSION="${KFAKE_VERSION:-}"
PPROF_ADDR=""
DATA_DIR=""
CUSTOM_TIMEOUT=""
RESTART_DELAY=""
KEEP_LOGS=""

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
KFAKE_DIR="$SCRIPT_DIR"
KGO_DIR="$SCRIPT_DIR/../kgo"
LOG_DIR="/tmp/kfake_test_logs"

while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--test)
            TEST_TYPE="$2"
            shift 2
            ;;
        -n|--iterations)
            MAX_ITERATIONS="$2"
            shift 2
            ;;
        -r|--records)
            RECORDS="$2"
            shift 2
            ;;
        --race)
            RACE="-race"
            shift
            ;;
        -l|--log-level)
            CLIENT_LOG="$2"
            SERVER_LOG_LEVEL="$2"
            shift 2
            ;;
        --client-log)
            CLIENT_LOG="$2"
            shift 2
            ;;
        --server-log)
            SERVER_LOG_LEVEL="$2"
            shift 2
            ;;
        -v|--version)
            KFAKE_VERSION="$2"
            shift 2
            ;;
        --pprof)
            PPROF_ADDR="$2"
            shift 2
            ;;
        --data-dir)
            DATA_DIR="$2"
            shift 2
            ;;
        --restart)
            RESTART_DELAY="$2"
            shift 2
            ;;
        --timeout)
            CUSTOM_TIMEOUT="$2"
            shift 2
            ;;
        --keep-logs)
            KEEP_LOGS=1
            shift
            ;;
        -k|--kill)
            echo "Killing processes on ports 9092, 9093, 9094..."
            for port in 9092 9093 9094; do
                pid=$(lsof -ti:$port 2>/dev/null)
                if [ -n "$pid" ]; then
                    echo "  Killing PID $pid on port $port"
                    kill $pid 2>/dev/null || true
                fi
            done
            echo "Done."
            exit 0
            ;;
        --clean)
            for port in 9092 9093 9094; do
                pid=$(lsof -ti:$port 2>/dev/null)
                if [ -n "$pid" ]; then
                    echo "Killing PID $pid on port $port"
                    kill $pid 2>/dev/null || true
                fi
            done
            rm -rf "$LOG_DIR"
            echo "Removed $LOG_DIR"
            if [ -n "$DATA_DIR" ] && [ -d "$DATA_DIR" ]; then
                rm -rf "$DATA_DIR"/*
                echo "Cleaned $DATA_DIR"
            fi
            exit 0
            ;;
        -h|--help)
            echo "Usage: ./run_tests.sh [options]"
            echo "Options:"
            echo "  -t, --test PATTERN     Test to run (default: all tests)"
            echo "                         Examples: Txn, Group, Txn/range, Group/sticky"
            echo "  -n, --iterations NUM   Max iterations (default: 50)"
            echo "  -r, --records NUM      Number of records (default: 500000)"
            echo "  --race                 Enable race detector (default: off)"
            echo "  -l, --log-level LEVEL  Set log level for both client and server"
            echo "  --client-log LEVEL     Set KGO_LOG_LEVEL for the test client only"
            echo "  --server-log LEVEL     Set kfake server log level only"
            echo "  -v, --version VERSION  Kafka version to emulate (e.g., 2.8, 3.5)"
            echo "  --pprof ADDR           Enable pprof on server (e.g., :6060)"
            echo "  --data-dir DIR         Persistence directory for kfake server"
            echo "  --restart SECS         Kill and restart server after SECS seconds"
            echo "  --timeout DURATION     Test timeout (default: 180s, 450s with --race)"
            echo "  --keep-logs            Keep per-iteration logs (client_N.log, server_N.log)"
            echo "  -k, --kill             Kill processes on ports 9092-9094 and exit"
            echo "  --clean                Kill servers and remove /tmp/kfake_test_logs"
            echo "  -h, --help             Show this help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

if [ -n "$RESTART_DELAY" ] && [ -z "$DATA_DIR" ]; then
    echo "ERROR: --restart requires --data-dir for state recovery"
    exit 1
fi

# Build test pattern
if [ -z "$TEST_TYPE" ]; then
    TEST_PATTERN=""
    RUN_ARG=""
else
    TEST_PATTERN="Test${TEST_TYPE}"
    RUN_ARG="-test.run $TEST_PATTERN"
fi
mkdir -p "$LOG_DIR"
SERVER_LOG="$LOG_DIR/server.log"
CLIENT_LOG_FILE="$LOG_DIR/client.log"
SERVER_BIN="$LOG_DIR/kfake-server"
TEST_BIN="$LOG_DIR/kgo-test"
SERVER_PID=""

# Build binaries once up front
echo "Building server binary..."
(cd "$KFAKE_DIR" && go build -o "$SERVER_BIN" main.go) || { echo "FAILED: server build"; exit 1; }

echo "Building test binary..."
(cd "$KGO_DIR" && go test -c $RACE -o "$TEST_BIN") || { echo "FAILED: test build"; exit 1; }

cleanup_interrupt() {
    echo ""
    echo "Interrupted. Server (pid $SERVER_PID) left running."
    echo "Use --clean to kill servers and remove logs."
    SERVER_PID=""
    exit 1
}
trap cleanup_interrupt SIGINT SIGTERM

# Check if a port is in use via a TCP connect attempt.
port_in_use() {
    (echo >/dev/tcp/127.0.0.1/$1) 2>/dev/null
}

if [ -n "$CUSTOM_TIMEOUT" ]; then
    TIMEOUT="$CUSTOM_TIMEOUT"
elif [ -n "$RACE" ]; then
    TIMEOUT="450s"
else
    TIMEOUT="180s"
fi

# Build common server args once.
SERVER_ARGS=""
if [ -n "$KFAKE_VERSION" ]; then
    SERVER_ARGS="$SERVER_ARGS --as-version $KFAKE_VERSION"
fi
if [ -n "$SERVER_LOG_LEVEL" ]; then
    SERVER_ARGS="$SERVER_ARGS -l $SERVER_LOG_LEVEL"
fi
if [ -n "$PPROF_ADDR" ]; then
    SERVER_ARGS="$SERVER_ARGS -pprof $PPROF_ADDR"
fi
if [ -n "$DATA_DIR" ]; then
    SERVER_ARGS="$SERVER_ARGS --data-dir $DATA_DIR"
fi
SERVER_ARGS="$SERVER_ARGS -c group.consumer.heartbeat.interval.ms=1000"

SERVER_PID_FILE="$LOG_DIR/server.pid"

# Wait for server to be listening on port 9092 (max 5s).
wait_for_server() {
    for _ in $(seq 1 50); do
        if port_in_use 9092; then
            return 0
        fi
        sleep 0.1
    done
    return 1
}

# Wait for port 9092 to be free (max 5s).
wait_for_port_free() {
    for _ in $(seq 1 50); do
        if ! port_in_use 9092; then
            return 0
        fi
        sleep 0.1
    done
    return 1
}

# Start the server, wait for it to listen, verify it's alive.
# Writes PID to SERVER_PID_FILE for coordination with restart loop.
start_server() {
    local log_mode="$1"  # "truncate" or "append"
    if [ "$log_mode" = "append" ]; then
        "$SERVER_BIN" $SERVER_ARGS >> "$SERVER_LOG" 2>&1 &
    else
        "$SERVER_BIN" $SERVER_ARGS > "$SERVER_LOG" 2>&1 &
    fi
    SERVER_PID=$!
    echo "$SERVER_PID" > "$SERVER_PID_FILE"

    wait_for_server
    if ! kill -0 $SERVER_PID 2>/dev/null; then
        echo "FAILED: Server crashed on startup"
        echo "Server log:"
        tail -20 "$SERVER_LOG"
        return 1
    fi
    return 0
}

# Kill server and wait for clean exit.
#
# Uses wait_for_exit (kill -0 polling) instead of bash `wait` because the
# server may have been started by restart_loop, which runs in a subshell;
# that makes the server a grand-child of main, and bash `wait` on a
# grand-child silently no-ops. Without a real wait, the caller's rm -rf
# and next start_server race the old server's still-in-flight saveToDisk
# (which writes *.tmp files and renames them) -- the new server then
# panics in persistInitialState with "rename *.tmp: no such file or
# directory" because both processes are competing on the same data dir.
# If the graceful shutdown overshoots the 10s poll window, SIGKILL.
#
# Then sweep ports 9092-9094 for any server process we failed to track.
# restart_loop can race with its own kill: if we killed restart_loop
# after it backgrounded a new server but before it wrote the pid to
# SERVER_PID_FILE, that server is a tracked-nowhere orphan that would
# otherwise still be writing to the data dir when the next iteration's
# rm -rf runs.
stop_server() {
    local pid
    pid=$(cat "$SERVER_PID_FILE" 2>/dev/null)
    if [ -n "$pid" ]; then
        kill "$pid" 2>/dev/null || true
        if ! wait_for_exit "$pid"; then
            kill -9 "$pid" 2>/dev/null || true
            wait_for_exit "$pid" || true
        fi
    fi
    for port in 9092 9093 9094; do
        local orphan
        orphan=$(lsof -ti:$port 2>/dev/null || true)
        if [ -n "$orphan" ]; then
            kill -9 "$orphan" 2>/dev/null || true
            wait_for_exit "$orphan" || true
        fi
    done
    SERVER_PID=""
}

# Wait for a process to exit (max 10s). Works across shell boundaries
# unlike `wait` which only works on children of the current shell.
wait_for_exit() {
    local pid=$1
    for _ in $(seq 1 100); do
        if ! kill -0 "$pid" 2>/dev/null; then
            return 0
        fi
        sleep 0.1
    done
    return 1
}

# Background restart loop: kills server after delay seconds,
# waits for clean shutdown, restarts. Repeats until killed.
# Uses SERVER_PID_FILE to coordinate the current PID.
restart_loop() {
    local delay=$1
    while true; do
        sleep "$delay"
        local pid
        pid=$(cat "$SERVER_PID_FILE" 2>/dev/null)
        if [ -z "$pid" ]; then
            continue
        fi
        echo "  [restart] Killing server (pid $pid) after ${delay}s..."
        kill "$pid" 2>/dev/null || true
        wait_for_exit "$pid"
        wait_for_port_free
        echo "  [restart] Starting server..."
        "$SERVER_BIN" $SERVER_ARGS >> "$SERVER_LOG" 2>&1 &
        local new_pid=$!
        echo "$new_pid" > "$SERVER_PID_FILE"
        wait_for_server
        if ! kill -0 "$new_pid" 2>/dev/null; then
            echo "  [restart] FAILED: Server crashed on restart"
            tail -20 "$SERVER_LOG"
            return 1
        fi
        echo "  [restart] Server back (pid $new_pid)"
    done
}

echo ""
echo "Configuration:"
echo "  Iterations: $MAX_ITERATIONS"
echo "  Records: $RECORDS"
echo "  Test pattern: ${TEST_PATTERN:-all}"
echo "  Race detector: ${RACE:-disabled}"
echo "  Client log level: ${CLIENT_LOG:-default}"
echo "  Server log level: ${SERVER_LOG_LEVEL:-default}"
echo "  Kafka version: ${KFAKE_VERSION:-latest}"
echo "  Pprof: ${PPROF_ADDR:-disabled}"
echo "  Data dir: ${DATA_DIR:-disabled}"
echo "  Restart: ${RESTART_DELAY:-disabled}${RESTART_DELAY:+s}"
echo "  Timeout: $TIMEOUT"
echo "  Keep logs: ${KEEP_LOGS:-disabled}"
echo "  Logs: $LOG_DIR"
echo ""

for i in $(seq 1 $MAX_ITERATIONS); do
    echo "=== Run $i of $MAX_ITERATIONS ==="
    RUN_START=$SECONDS

    start_server "truncate" || exit 1

    # If --restart is set, run the restart loop in background.
    RESTARTER_PID=""
    if [ -n "$RESTART_DELAY" ]; then
        restart_loop "$RESTART_DELAY" &
        RESTARTER_PID=$!
    fi

    # Run the test
    KGO_TEST_RECORDS=$RECORDS KGO_LOG_LEVEL=$CLIENT_LOG "$TEST_BIN" $RUN_ARG -test.timeout $TIMEOUT > "$CLIENT_LOG_FILE" 2>&1
    TEST_EXIT=$?

    # Stop restart loop if running
    if [ -n "$RESTARTER_PID" ]; then
        kill $RESTARTER_PID 2>/dev/null || true
        wait $RESTARTER_PID 2>/dev/null
    fi

    if [ $TEST_EXIT -ne 0 ]; then
        # Archive failure logs into a timestamped subdir so subsequent
        # run_tests.sh invocations don't overwrite them.
        FAIL_DIR="$LOG_DIR/fail_$(date +%Y%m%d_%H%M%S)_run${i}"
        mkdir -p "$FAIL_DIR"
        cp "$CLIENT_LOG_FILE" "$FAIL_DIR/client.log" 2>/dev/null
        cp "$SERVER_LOG" "$FAIL_DIR/server.log" 2>/dev/null
        echo "Archived failure logs to $FAIL_DIR"
        if [ -n "$KEEP_LOGS" ]; then
            cp "$CLIENT_LOG_FILE" "$LOG_DIR/client_${i}.log"
            cp "$SERVER_LOG" "$LOG_DIR/server_${i}.log"
            echo "Saved logs: client_${i}.log, server_${i}.log"
        fi
        echo "FAILED on run $i"
        echo "Client log: $CLIENT_LOG_FILE"
        echo "Server log: $SERVER_LOG"
        echo ""
        echo "=== Last 50 lines of client log ==="
        tail -50 "$CLIENT_LOG_FILE"
        echo ""
        echo "Server (pid $SERVER_PID) left running for debugging."
        echo "Connect to localhost:9092 to inspect state."
        echo "Kill manually when done: kill $SERVER_PID"
        SERVER_PID=""  # Clear so EXIT trap doesn't kill it
        exit 1
    fi

    if [ -n "$KEEP_LOGS" ]; then
        cp "$CLIENT_LOG_FILE" "$LOG_DIR/client_${i}.log"
        cp "$SERVER_LOG" "$LOG_DIR/server_${i}.log"
    fi

    stop_server
    if [ -n "$DATA_DIR" ]; then
        rm -rf "$DATA_DIR"/*
    fi
    echo "PASS (run $i) - $((SECONDS - RUN_START))s"
done

echo ""
echo "SUCCESS: All $MAX_ITERATIONS iterations passed!"
