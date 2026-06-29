#!/usr/bin/env bash
#
# Gate govulncheck JSON results against an ignore list.
#
# govulncheck with -format=json ALWAYS exits 0 (regardless of findings), so this
# script parses its streaming JSON output and decides pass/fail itself:
#
#   * FAIL (exit 1) if any *reachable* vulnerability is reported whose OSV id is
#     not in the ignore list. "Reachable" means govulncheck traced a call into
#     the vulnerable symbol — the sink frame (trace[0]) has a "function". This
#     mirrors the exit-code-3 semantics of govulncheck's default text mode and
#     ignores informational/module-only findings.
#   * PASS (exit 0) otherwise. IDs present in the ignore list are reported as
#     suppressed; ignore-list entries that match nothing are warned about so the
#     list stays current.
#
# Written for portability across bash 3.2+ (no mapfile, no associative arrays).
#
# Usage: govulncheck-filter.sh <govulncheck-json> <ignore-file>

set -euo pipefail

json="${1:?usage: govulncheck-filter.sh <govulncheck-json> <ignore-file>}"
ignore_file="${2:-}"

command -v jq >/dev/null || { echo "error: jq is required" >&2; exit 2; }

# Fail closed if the input is not a recognizable govulncheck JSON stream. Without
# this, an empty/truncated file (govulncheck crashed mid-write) or a future
# change to govulncheck's output shape would make the jq filter below match
# nothing, yielding a silent "no vulnerabilities" pass — the worst failure
# direction for a security gate. govulncheck always emits a `config` message
# first, so its absence means we are not looking at trustworthy output.
jq -e 'select(.config != null)' "${json}" >/dev/null 2>&1 || {
  echo "::error::'${json}' is not a valid govulncheck JSON stream (no config message) — failing closed" >&2
  exit 2
}

# Normalize the ignore list to "ID<TAB>justification" lines (comments stripped),
# stored in a temp file so we can look entries up without associative arrays.
ignore_norm="$(mktemp)"
trap 'rm -f "${ignore_norm}"' EXIT
if [[ -n "${ignore_file}" ]] && [[ -f "${ignore_file}" ]]; then
  sed 's/\r$//' "${ignore_file}" \
    | { grep -vE '^[[:space:]]*(#|$)' || true; } \
    | while IFS= read -r line; do
        id="$(printf '%s' "${line%%#*}" | tr -d '[:space:]')"
        [[ -z "${id}" ]] && continue
        reason="${line#*#}"
        [[ "${reason}" = "${line}" ]] && reason="(no justification given)"
        reason="$(printf '%s' "${reason}" | sed 's/^[[:space:]]*//')"
        printf '%s\t%s\n' "${id}" "${reason}"
      done > "${ignore_norm}"
fi
ignored_ids() { cut -f1 "${ignore_norm}"; }
reason_for()  { awk -F'\t' -v id="$1" '$1==id{$1="";sub(/^\t/,"");print;exit}' "${ignore_norm}"; }

# Distinct OSV ids of reachable findings (the vulnerable symbol is actually called).
# Assumption (govulncheck v1.x source mode): for every *called* vulnerability,
# govulncheck emits at least one finding whose sink frame (trace[0]) carries a
# "function". Re-validate this filter when bumping GOVULNCHECK_VERSION.
reachable="$(jq -r 'select(.finding.trace[0].function != null) | .finding.osv' "${json}" | sort -u)"

fail="" ; suppressed=""
while IFS= read -r id; do
  [[ -z "${id}" ]] && continue
  if cut -f1 "${ignore_norm}" | grep -qxF "${id}"; then
    suppressed="${suppressed}${id}"$'\n'
  else
    fail="${fail}${id}"$'\n'
  fi
done <<EOF
${reachable}
EOF

# Hygiene: flag ignore-list entries that no longer match any finding.
while IFS= read -r id; do
  [[ -z "${id}" ]] && continue
  printf '%s\n' "${reachable}" | grep -qxF "${id}" \
    || echo "::warning::ignore-list entry ${id} matched no finding — remove it from ${ignore_file}"
done <<EOF
$(ignored_ids)
EOF

if [[ -n "${suppressed}" ]]; then
  echo "Suppressed by ignore list:"
  printf '%s' "${suppressed}" | while IFS= read -r id; do
    [[ -z "${id}" ]] && continue
    echo "  - ${id}: $(reason_for "${id}")"
  done
fi

if [[ -n "${fail}" ]]; then
  echo
  echo "::error::govulncheck found reachable vulnerabilities not in the ignore list:"
  printf '%s' "${fail}" | while IFS= read -r id; do
    [[ -z "${id}" ]] && continue
    echo "  x ${id}  (https://pkg.go.dev/vuln/${id})"
    { jq -r --arg id "${id}" '
        select(.finding.osv == $id and .finding.trace[0].function != null)
        | .finding
        | "      call: " + ([.trace[] | (.package // .module) + (if .function then "." + .function else "" end)] | reverse | join(" -> "))
      ' "${json}" | sort -u | head -n 5; } || true
  done
  exit 1
fi

echo "OK: no un-ignored reachable vulnerabilities."
