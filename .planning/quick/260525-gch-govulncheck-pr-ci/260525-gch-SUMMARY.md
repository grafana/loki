---
phase: quick-260525-gch-govulncheck-pr-ci
plan: 01
subsystem: ci
tags: [ci, security, govulncheck, github-actions, sha-pinning]
dependency_graph:
  requires: []
  provides:
    - "PR-gating govulncheck workflow"
    - "First SHA-pinned GitHub Actions workflow (pattern reference for future security workflows)"
  affects:
    - ".github/workflows/"
tech_stack:
  added:
    - "golang.org/x/vuln/cmd/govulncheck (installed at workflow runtime)"
  patterns:
    - "SHA-pinned GitHub Actions (commit SHA + trailing `# vX.Y.Z` comment)"
    - "Top-level workflow permissions block (contents: read)"
    - "Path-filtered pull_request triggers"
    - "Explicit Go module + build cache management (cache: false on setup-go)"
key_files:
  created:
    - ".github/workflows/govulncheck.yml"
  modified: []
decisions:
  - "actions/cache stayed on @v4 tag rather than SHA-pinning — task scope only called out checkout and setup-go as the security-pinned actions; cache is a build-acceleration concern, not a security boundary."
  - "go install ...@latest for govulncheck — keeps the scanner current with vuln-DB tooling; the vuln database itself is always fetched fresh by the tool at runtime, so pinning the scanner version is low-value."
  - "ubuntu-latest runner (not ubuntu-x64 / arm64-large) — govulncheck is CPU-light; stock runner is more portable and avoids consuming reserved capacity."
  - "Single root go.sum for cache key — explicitly out-of-scope for cmd/dataobj-inspect and operator submodules; covering them now would create cache-key churn without a corresponding scan step."
metrics:
  duration: "1m 30s"
  completed: "2026-05-25"
---

# Quick Task 260525-gch: govulncheck PR CI Summary

Added `.github/workflows/govulncheck.yml` — a PR-gating workflow that runs `govulncheck ./...` against the root Loki Go module on any PR touching Go sources or module files, with SHA-pinned actions and a minimal `contents: read` permission set. First SHA-pinned workflow in the repo; establishes the pattern for future security-focused CI.

## Final Artifact

- **Path:** `.github/workflows/govulncheck.yml`
- **Line count:** 64 (verified with `wc -l`)
- **Commit:** `4deb4fcfa4` (message: `ci: add govulncheck PR workflow`)

## Verification Results

All checks from the plan's `<verify>` block and the prompt's verification list passed:

| # | Check | Result |
|---|-------|--------|
| 1 | `test -f .github/workflows/govulncheck.yml` | PASS |
| 2 | `actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683` SHA pin present | PASS |
| 3 | `actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5` SHA pin present | PASS |
| 4 | `go-version: "1.26.2"` present | PASS |
| 5 | `govulncheck ./...` invocation present | PASS |
| 6 | SHA-pinned `uses:` count | 2 (expected 2) — PASS |
| 7 | Tag-pinned `uses:` count (`actions/cache@v4` only) | 1 (expected 1) — PASS |
| 8 | `actionlint` available? | **Not installed locally** — manual YAML review performed; structure parsed via Ruby's `YAML.load_file` and confirmed: 5 ordered steps (Checkout → Setup Go → Cache → Install govulncheck → Run govulncheck), top-level `permissions: {contents: read}`, `pull_request.paths` covers `**.go`, `**/go.mod`, `**/go.sum` |
| 9 | `go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck -version` | PASS |
| – | `contents: read` permission present | PASS |
| – | `paths:` filter contains `**.go` | PASS |
| – | `paths:` filter contains `**/go.sum` | PASS |

### govulncheck local install output

```
Go: go1.26.3
Scanner: govulncheck@v1.3.0
DB: https://vuln.go.dev
DB updated: 2026-05-22 18:28:47 +0000 UTC
```

This proves the `go install ...@latest && govulncheck -version` pair from steps (d) of the workflow is well-formed; the workflow will install the same scanner during CI runs. Note: we did NOT run `govulncheck ./...` locally — that's intentionally the workflow's job on the first PR, where the finding count will be observed for real.

### YAML structural validation (substitute for actionlint)

Ruby `YAML.load_file` parse confirmed:

- `name: govulncheck`
- `on:` triggers: `["pull_request"]` only — no push, no schedule, no workflow_dispatch
- `permissions: {"contents" => "read"}`
- `jobs: ["govulncheck"]`
- `runs-on: ubuntu-latest`
- `timeout-minutes: 15`
- Step count: 5 (in the exact order: Checkout code → Setup Go → Cache Go modules and build cache → Install govulncheck → Run govulncheck)

## Deviations from Plan

None — plan executed exactly as written. The only judgment call was the placement of the YAML header comments (kept as `#` lines above `name:`, no `---` document marker); the planner's spec called for "a short YAML comment block (lines starting `#`)" and a literal `---` would have added a YAML document-start marker that adds noise without value. Final file leads with the comment block then `name:`, matching standard GitHub Actions style.

## Deferred Follow-Ups

Copied from the workflow file's own header comment so they're discoverable from this SUMMARY:

- **`cmd/dataobj-inspect/` separate go.mod** — has its own module graph; needs its own govulncheck invocation in a follow-up plan.
- **`operator/` separate go.mod** — same deal; out of scope for this first workflow.
- **`loki-build-image` integration** — user flagged for later; current workflow installs govulncheck directly via `go install` to avoid build-image dependency.
- **Baseline / diff-against-main logic** — deliberately deferred until we observe the first real finding count on a PR; building a baseline before we know the noise floor is premature.
- **Any non-govulncheck steps (no tests, no lint, no build)** — workflow has exactly one assertion by design.

## First-PR Finding Count

**Unknown.** Will be observed when this workflow first runs against a real PR. The job is configured to fail on any non-zero govulncheck exit, so if there are findings in current dependencies, the first PR that touches Go files will surface them.

## Self-Check: PASSED

- `.github/workflows/govulncheck.yml` — FOUND
- Commit `4deb4fcfa4` — FOUND in `git log`
- No deletions in commit (verified via `git diff --diff-filter=D HEAD~1 HEAD`)
- No untracked files after commit
