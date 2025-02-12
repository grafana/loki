#!/usr/bin/env bash

# Source utility functions and logging helpers
source "$(pwd)/tools/includes/utils.sh"
source "./tools/includes/logging.sh"

# output the heading
heading "Loki Mixin Tests" "Running Loki Mixin Tests"

# check to see if logcli is installed
if [[ "$(command -v logcli)" = "" ]]; then
  emergency "logcli command is required, see: (https://github.com/grafana/loki/releases) or run: brew install logcli";
fi

# check to see if jsonnet is installed
if [[ "$(command -v jsonnet)" = "" ]]; then
  emergency "jsonnet command is required, see: (https://github.com/google/go-jsonnet) or run: go install github.com/google/go-jsonnet/cmd/jsonnet@latest";
fi

# Check for filter argument
FILTER=${1:-""}

# ANSI escape codes for colored output
RED=$'\e[0;31m'    # Regular red text
GREEN=$'\e[0;32m'  # Regular green text
YELLOW=$'\e[0;33m'   # Yellow for errors
BOLD=$'\e[1m'    # Bold/bright text
DIM=$'\e[2m'     # Dimmed/faded text
CYAN=$'\e[0;36m'   # Cyan for suite names
NC=$'\e[0m'      # Reset all formatting

# Unicode symbols for status indicators
PASS="✓"
FAIL="✗"
ERROR="!"

# Initialize counters as global variables
TOTAL_FILES=0
TOTAL_TESTS=0
TOTAL_GROUPS=0
TOTAL_PASSED=0
TOTAL_FAILED=0
TOTAL_ERRORS=0
TOTAL_FAILED_EXECUTIONS=0
CURRENT_SUITE_NUM=0

# Validate LogQL selector using logcli fmt
# Args:
#   $1 - selector string to validate
validate_selector() {
  local selector="$1"
  local fmt_output
  if ! fmt_output=$(echo "$selector" | logcli fmt --stdin 2>&1); then
    # Remove timestamp (first 20 characters) and return error
    echo "${fmt_output:20}"
    return 1
  fi
  return 0
}

# Format test results with colored pass/fail/error indicators
# Args:
#   $1 - success: boolean indicating if all tests passed
#   $2 - passed: number of passing tests
#   $3 - total: total number of tests
#   $4 - errors: number of errors (optional)
#   $5 - failed_executions: number of failed LogQL validations (optional)
format_results() {
  local success=$1
  local passed=$2
  local total=$3
  local errors=${4:-0}
  local failed_executions=${5:-0}
  local failed=$((total - passed))

  local result=""
  if [ "$errors" -gt 0 ]; then
    result="${YELLOW}${ERROR} ${passed} / ${total} passed (${failed} failed, ${errors} errors, ${failed_executions} failed executions)${NC}"
  elif [ "$success" = true ]; then
    result="${GREEN}${PASS} ${passed} / ${total} passed${NC}"
  else
    result="${RED}${FAIL} ${passed} / ${total} passed (${failed} failed, ${failed_executions} failed executions)${NC}"
  fi
  echo -e "$result"
}

# humanize_duration
# -----------------------------------
# Converts an input to a human readable duration
# -----------------------------------
humanize_duration() {
  local value="${1}"
  local unit="${2:-nano}"
  local precision="${3:-2}"
  local decimals="${4:-0}"

  python3 -c "
import sys

def humanize_duration(value, unit, precision, decimals):
    units_in_seconds = {
        'nano': 1e-9,
        'micro': 1e-6,
        'millis': 1e-3,
        'seconds': 1,
        'minutes': 60,
        'hours': 3600,
    }

    if unit not in units_in_seconds:
        print('Invalid unit. Use \"nano\", \"micro\", \"millis\", \"seconds\", \"minutes\", or \"hours\".', file=sys.stderr)
        sys.exit(1)

    total_seconds = value * units_in_seconds[unit]
    milliseconds = total_seconds * 1000

    sign = '-' if milliseconds < 0 else ''
    milliseconds = abs(milliseconds)
    ms = int(milliseconds % 1000)
    seconds = int(milliseconds // 1000 % 60)
    minutes = int(milliseconds // (1000 * 60) % 60)
    hours = int(milliseconds // (1000 * 60 * 60) % 24)
    days = int(milliseconds // (1000 * 60 * 60 * 24))

    result = []
    if days > 0:
        result.append(f'{sign}{days}d{hours}h{minutes}m{seconds}s')
    elif hours > 0:
        result.append(f'{sign}{hours}h{minutes}m{seconds}s')
    elif minutes > 0:
        result.append(f'{sign}{minutes}m{seconds}s')
    elif seconds > 0:
        result.append(f'{sign}{seconds}.{ms:03d}s' if ms else f'{sign}{seconds}s')
    elif milliseconds > 0:
        result.append(f'{sign}{milliseconds:.{decimals}f}ms')
    else:
        result.append('0s')

    return ' '.join(result[:precision])

value = int(sys.argv[1])
unit = sys.argv[2] if len(sys.argv) > 2 else 'nano'
precision = int(sys.argv[3]) if len(sys.argv) > 3 else 2
decimals = int(sys.argv[4]) if len(sys.argv) > 4 else 0
print(humanize_duration(value, unit, precision, decimals))

" "${value}" "${unit}" "${precision}" "${decimals}"
}

# ts_epoch_nano
# -----------------------------------
# Function to get current time in epoch format with nanoseconds using Python
# -----------------------------------
ts_epoch_nano() {
  python3 -c 'import time; print(time.time_ns())'
}

# Process a single test file
# Args:
#   $1 - test file path
process_test_file() {
  local test_file="${1}"
  local file_name
  file_name=$(basename "${test_file}")
  local relative_path="tests/${test_file#*tests/}"
  local suite_total=0
  local suite_passed=0
  local suite_failed=0
  local suite_failed_executions=0
  local suite_start_time
  suite_start_time=$(ts_epoch_nano)

  # Run jsonnet directly on the test file
  local test_output
  if ! test_output=$(jsonnet "${test_file}" 2>&1); then
    echo -e "${YELLOW}${ERROR} Execution Error in ${CYAN}${file_name}${NC}:${NC}"
    echo "${test_output}"
    ((TOTAL_ERRORS += 1))
    return
  fi

  # Handle empty or invalid JSON output
  if ! echo "${test_output}" | jq empty 2>/dev/null; then
    echo -e "${YELLOW}${ERROR} Invalid JSON output in ${CYAN}${file_name}${NC}:${NC}"
    echo "${test_output}"
    ((TOTAL_ERRORS += 1))
    return
  fi

  local suite_name
  suite_name=$(echo "${test_output}" | jq -r '.name // "Unnamed Test Suite"')
  ((CURRENT_SUITE_NUM += 1))
  echo -e "\n${DIM}(${CURRENT_SUITE_NUM}/${TOTAL_FILES})${NC} ${BOLD}Suite: ${CYAN}${suite_name}${NC} || File: ${DIM}${relative_path}${NC}"
  echo "  Groups:"

  # Process groups and their results
  if echo "${test_output}" | jq -e '.tests[]?' >/dev/null 2>&1; then
    while read -r group_b64; do
      [[ -z "${group_b64}" ]] && continue
      local group_start_time
      group_start_time=$(ts_epoch_nano)

      group=$(echo "${group_b64}" | base64 --decode)
      name=$(echo "${group}" | jq -r '.name // "Unnamed Group"')
      total=$(echo "${group}" | jq -r '.cases | length')
      passed=$(echo "${group}" | jq -r '[.cases[] | select(.test == .expected)] | length')
      local group_failed=0
      local group_failed_executions=0
      local logql_errors=""

      ((suite_total += total))

      # Only validate LogQL format for test cases that passed the unit test
      while read -r case_b64; do
        [[ -z "${case_b64}" ]] && continue
        case=$(echo "${case_b64}" | base64 --decode)
        test_selector=$(echo "${case}" | jq -r '.test')
        case_name=$(echo "${case}" | jq -r '.name')

        # Skip LogQL validation if the test case failed
        if [[ "$(echo "${case}" | jq -r '.test')" == "$(echo "${case}" | jq -r '.expected')" ]]; then
          # Add brackets if they're not present
          if [[ ! "${test_selector}" =~ ^\{.*\}$ ]]; then
            test_selector="{${test_selector}}"
          fi

          local validation_output
          if [[ "${test_selector}" != "{}" ]] && ! validation_output=$(validate_selector "${test_selector}"); then
            ((group_failed_executions += 1))
            ((suite_failed_executions += 1))
            logql_errors+="    ${BOLD}Test Case:${NC} ${case_name}\n"
            logql_errors+="    ${RED}LogQL Format Error:${NC}\n"
            logql_errors+="    ${DIM}${validation_output}${NC}\n\n"
          fi
        fi
      done < <(echo "${group}" | jq -r '.cases[]? | @base64')

      # Update passed count based on both test matches and LogQL validation
      local actual_passed=$((passed - group_failed_executions))
      ((group_failed = total - actual_passed))
      ((suite_failed += group_failed))

      local group_end_time
      group_end_time=$(ts_epoch_nano)
      local group_duration
      group_duration=$(humanize_duration $((group_end_time - group_start_time)))

      if [[ "${group_failed}" -eq 0 && "${group_failed_executions}" -eq 0 ]]; then
        printf "    ${GREEN}✓ %d / %d passed: %s${NC} in ${DIM}%s${NC}\n" "${actual_passed}" "${total}" "${name}" "${group_duration}"
      else
        printf "    ${RED}✗ %d / %d passed: %s${NC} in ${DIM}%s${NC}\n" "${actual_passed}" "${total}" "${name}" "${group_duration}"
      fi

      # Display test failures
      if [[ "${group_failed}" -gt 0 ]]; then
        echo "${group}" | jq -r '.cases[] | select(.test != .expected) | "      \u001b[1mTest Case:\u001b[0m \(.name)\n      \u001b[0;31mFAILED:\u001b[0m Unit Test\n      \u001b[2m- Expected:\u001b[0m  \(.expected)\n      \u001b[2m- Actual:\u001b[0m    \(.test)\n"'
      fi

      # Display LogQL format errors
      if [[ -n "${logql_errors}" ]]; then
        echo -e "${logql_errors}"
      fi
    done < <(echo "$test_output" | jq -r '.tests[]? | @base64')
  fi

  ((suite_passed = suite_total - suite_failed))

  local suite_end_time
  suite_end_time=$(ts_epoch_nano)
  local suite_duration
  suite_duration=$(humanize_duration $((suite_end_time - suite_start_time)))

  # Update result display
  if [[ "${suite_failed}" -eq 0 && "${suite_failed_executions}" -eq 0 ]]; then
    printf "  ${BOLD}Result:${NC}  ${GREEN}✓ %d / %d passed${NC} in ${DIM}%s${NC}\n" "${suite_passed}" "${suite_total}" "${suite_duration}"
  else
    printf "  ${BOLD}Result:${NC}  ${RED}✗ %d / %d passed (%d failed executions)${NC} in ${DIM}%s${NC}\n" "${suite_passed}" "${suite_total}" "${suite_failed_executions}" "${suite_duration}"
  fi

  # Update global counters
  ((TOTAL_GROUPS += $(echo "${test_output}" | jq -r '.tests | length // 0' || true)))
  ((TOTAL_TESTS += suite_total))
  ((TOTAL_PASSED += suite_passed))
  ((TOTAL_FAILED += suite_failed))
  ((TOTAL_FAILED_EXECUTIONS += suite_failed_executions))
}

# Find and process all test files
find_command="find \"$(pwd)/tests\" -type f -name \"*.libsonnet\" -not -name \"*.test.libsonnet\""
if [[ -n "${FILTER}" ]]; then
  find_command="${find_command} | grep \"${FILTER}\""
fi

# Add sort to ensure consistent ordering
find_command="${find_command} | sort"

TOTAL_FILES=$(eval "${find_command}" | wc -l | xargs || true)

# Only proceed if we found matching files
if [[ "${TOTAL_FILES}" -eq 0 ]]; then
  echo -e "${YELLOW}No test files found matching filter: ${FILTER}${NC}"
  exit 1
fi

# Start overall timing
OVERALL_START_TIME=$(ts_epoch_nano)

# Process matching test files
while read -r test_file; do
  process_test_file "${test_file}"
done < <(eval "${find_command}" || true)

# Calculate overall duration
OVERALL_END_TIME=$(ts_epoch_nano)
OVERALL_DURATION=$(humanize_duration $((OVERALL_END_TIME - OVERALL_START_TIME)))

# Print overall summary with colors
echo -e "\n${BOLD}Overall Summary${NC} (${DIM}${OVERALL_DURATION}${NC})\n${BOLD}===============================${NC}"
echo -e "${CYAN}Total Files:${NC}  ${BOLD}${TOTAL_FILES}${NC}"
echo -e "${CYAN}Total Groups:${NC} ${BOLD}${TOTAL_GROUPS}${NC}"
echo -e "${CYAN}Total Tests:${NC}  ${BOLD}${TOTAL_TESTS}${NC}"
if [[ "${TOTAL_PASSED}" -eq "${TOTAL_TESTS}" ]]; then
  echo -e "${CYAN}Total Passed:${NC} ${GREEN}${BOLD}${TOTAL_PASSED}${NC}"
else
  echo -e "${CYAN}Total Passed:${NC} ${YELLOW}${BOLD}${TOTAL_PASSED}${NC}"
fi
if [[ "${TOTAL_FAILED}" -eq 0 ]]; then
  echo -e "${CYAN}Total Failed:${NC} ${GREEN}${BOLD}${TOTAL_FAILED}${NC}"
else
  echo -e "${CYAN}Total Failed:${NC} ${RED}${BOLD}${TOTAL_FAILED}${NC}"
fi
if [[ "${TOTAL_ERRORS}" -eq 0 ]]; then
  echo -e "${CYAN}Total Errors:${NC} ${GREEN}${BOLD}${TOTAL_ERRORS}${NC}"
else
  echo -e "${CYAN}Total Errors:${NC} ${RED}${BOLD}${TOTAL_ERRORS}${NC}"
fi
if [[ "${TOTAL_FAILED_EXECUTIONS}" -eq 0 ]]; then
  echo -e "${CYAN}Total Failed Executions:${NC} ${GREEN}${BOLD}${TOTAL_FAILED_EXECUTIONS}${NC}"
else
  echo -e "${CYAN}Total Failed Executions:${NC} ${RED}${BOLD}${TOTAL_FAILED_EXECUTIONS}${NC}"
fi

# Exit with appropriate status code
# Exit 1 if there are any failures, errors, or failed executions, 0 if all tests passed
[[ "${TOTAL_FAILED}" -eq 0 && "${TOTAL_ERRORS}" -eq 0 && "${TOTAL_FAILED_EXECUTIONS}" -eq 0 ]] && exit 0 || exit 1
