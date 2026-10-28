#!/bin/bash
set -u
cd "$(dirname "$0")"
mkdir -p fuzz_results/crashes
LOG=fuzz_results/campaign.log
: > "$LOG"

TARGETS=(
  DeleteNative
  ParseStringNative
  EachKeyNative
  SetNative
  ObjectEachNative
  ParseFloatNative
  ParseIntNative
  ParseBoolNative
  TokenStartNative
  GetStringNative
  GetFloatNative
  GetIntNative
  GetBooleanNative
  GetUnsafeStringNative
)
PER_TARGET=${PER_TARGET:-180s}
MAX_PASSES=${MAX_PASSES:-4}

count_crashes() { ls fuzz_results/crashes/ 2>/dev/null | wc -l | tr -d ' '; }

prev_count=$(count_crashes)
pass=0
while [ $pass -lt $MAX_PASSES ]; do
  pass=$((pass+1))
  echo "=== $(date '+%H:%M:%S') PASS $pass start (per-target=$PER_TARGET, baseline_unique_crashes=$prev_count) ===" | tee -a "$LOG"
  for t in "${TARGETS[@]}"; do
    pre=$(count_crashes)
    echo "  -> $(date '+%H:%M:%S') Fuzz${t}" | tee -a "$LOG"
    go test -run='^$' -fuzz="^Fuzz${t}$" -fuzztime="${PER_TARGET}" \
        > "fuzz_results/${t}_pass${pass}.log" 2>&1
    rc=$?
    post=$(count_crashes)
    delta=$((post - pre))
    summary=$(tail -n 3 "fuzz_results/${t}_pass${pass}.log" | tr '\n' ' | ')
    echo "     rc=$rc new_crashes=$delta total=$post :: $summary" | tee -a "$LOG"
  done
  new_count=$(count_crashes)
  pass_delta=$((new_count - prev_count))
  echo "=== $(date '+%H:%M:%S') PASS $pass done: total_unique_crashes=$new_count (+$pass_delta this pass) ===" | tee -a "$LOG"
  if [ $pass_delta -eq 0 ] && [ $pass -ge 2 ]; then
    echo "=== SATURATED after pass $pass — no new crashes ===" | tee -a "$LOG"
    break
  fi
  prev_count=$new_count
done
echo "=== $(date '+%H:%M:%S') CAMPAIGN COMPLETE total_unique_crashes=$(count_crashes) ===" | tee -a "$LOG"
