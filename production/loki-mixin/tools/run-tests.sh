#!/usr/bin/env bash

# Source utility functions and logging helpers
source "$(pwd)/tools/includes/utils.sh"
source "./tools/includes/logging.sh"

# ANSI escape codes for colored output
RED=$'\e[0;31m'      # Regular red text
GREEN=$'\e[0;32m'    # Regular green text
BOLD=$'\e[1m'        # Bold/bright text
DIM=$'\e[2m'         # Dimmed/faded text
CYAN=$'\e[0;36m'     # Cyan for suite names
NC=$'\e[0m'          # Reset all formatting

# Unicode symbols for pass/fail indicators
PASS="✓"
FAIL="✗"

# Output the test suite heading
heading "Loki Mixin" "Running Tests"

# Run the jsonnet test file and capture the JSON output
test_output=$(jsonnet "$(pwd)/unit-tests.jsonnet")

# Format test results with colored pass/fail indicators
# Args:
#   $1 - success: boolean indicating if all tests passed
#   $2 - passed: number of passing tests
#   $3 - total: total number of tests
format_results() {
    local success=$1
    local passed=$2
    local total=$3
    local failed=$((total - passed))

    # Use green checkmark for all passing, red X with failure count otherwise
    if [ "$success" = true ]; then
        echo -e "${GREEN}${PASS} ${passed} / ${total} passed${NC}"
    else
        echo -e "${RED}${FAIL} ${passed} / ${total} passed (${failed} failed)${NC}"
    fi
}

# Process each test suite from the JSON output
echo "$test_output" | jq -r '.suites[] | @base64' | while read -r suite_b64; do
    # Decode the base64 suite data
    suite=$(echo "$suite_b64" | base64 --decode)

    # Get suite details
    name=$(echo "$suite" | jq -r '.name')
    total=$(echo "$suite" | jq -r '.summary.total')
    passed=$(echo "$suite" | jq -r '.summary.passed')
    success=$([[ "$total" -eq "$passed" ]] && echo true || echo false)

    # Print suite results and name (entire "Suite: name" part in bold cyan)
    printf "\n%s - ${BOLD}Suite: ${CYAN}%s${NC}\n" "$(format_results $success $passed $total)" "${name}"

    # Process each test group in the suite
    echo "$suite" | jq -r '.groups[] | @base64' | while read -r group_b64; do
        # Decode the base64 group data
        group=$(echo "$group_b64" | base64 --decode)

        # Extract group details
        name=$(echo "$group" | jq -r '.name')
        total=$(echo "$group" | jq -r '.summary.total')
        passed=$(echo "$group" | jq -r '.summary.passed')
        success=$([[ "$total" -eq "$passed" ]] && echo true || echo false)

        # Print group results and name (slightly dimmed)
        printf "      %s - Group: ${DIM}%s${NC}\n" "$(format_results $success $passed $total)" "${name}"

        # If there are failures, print details for each failing test
        if [ "$success" = false ]; then
            echo "$group" | jq -r '.results[] | select(.success==false) | "    \u001b[0;31mFAILED:\u001b[0m \(.name)\n    \u001b[2m- Expected:\u001b[0m \(.expected)\n    \u001b[2m- Actual:\u001b[0m   \(.actual)"'
        fi
    done
done

# Print overall test summary (in bold)
total=$(echo "$test_output" | jq -r '.summary.total')
passed=$(echo "$test_output" | jq -r '.summary.passed')
success=$([[ "$total" -eq "$passed" ]] && echo true || echo false)

echo -e "\n${BOLD}Overall Summary:${NC} $(format_results "$success" "$passed" "$total")"

# Exit with appropriate status code
[[ "$success" = true ]] && exit 0 || exit 1
