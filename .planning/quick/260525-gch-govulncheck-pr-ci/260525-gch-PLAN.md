---
phase: quick-260525-gch-govulncheck-pr-ci
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - .github/workflows/govulncheck.yml
autonomous: true
requirements:
  - QUICK-govulncheck-pr-ci
must_haves:
  truths:
    - "Opening a PR that touches any Go file (**.go, **/go.mod, **/go.sum) triggers the govulncheck workflow"
    - "Opening a PR that touches only non-Go files (e.g. *.md, helm charts) does NOT trigger the workflow"
    - "The workflow runs govulncheck against the root Loki module (./...) on Go 1.26.2"
    - "The workflow fails the PR check when govulncheck exits non-zero (any reachable vulnerability)"
    - "Both actions/checkout and actions/setup-go are pinned to commit SHAs, not floating tags"
    - "Top-level permissions block grants contents: read only"
    - "Module cache (~/go/pkg/mod) and build cache (~/.cache/go-build) are keyed on go.sum"
  artifacts:
    - path: ".github/workflows/govulncheck.yml"
      provides: "GitHub Actions workflow that runs govulncheck on Go-touching PRs"
      contains: "govulncheck ./..."
  key_links:
    - from: ".github/workflows/govulncheck.yml"
      to: "github.com/actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683"
      via: "uses: directive with SHA pin"
      pattern: "actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2"
    - from: ".github/workflows/govulncheck.yml"
      to: "github.com/actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5"
      via: "uses: directive with SHA pin"
      pattern: "actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5 # v5.5.0"
    - from: ".github/workflows/govulncheck.yml"
      to: "golang.org/x/vuln/cmd/govulncheck"
      via: "go install + govulncheck ./... invocation"
      pattern: "govulncheck ./..."
---

<objective>
Add a PR-gating GitHub Actions workflow that runs `govulncheck` against the
root Loki module whenever a PR touches Go sources or module files.

Purpose: Catch known-CVE reachable vulnerabilities in dependencies on PRs,
before merge, without depending on the loki-build-image. This is the first
SHA-pinned workflow in the repo and sets the security-hardening pattern that
future security workflows should follow.

Output: A single new file at `.github/workflows/govulncheck.yml`.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@.github/workflows/logql-correctness.yml
@.github/workflows/secret-scanning.yml
@.github/workflows/snyk.yml

<!-- Reference data — already verified against GitHub API by the planner. -->
<!-- DO NOT change these SHAs without re-verifying via the GitHub API. -->

actions/checkout — pin to commit `11bd71901bbe5b1630ceea73d27597364c9af683` (tag v4.2.2).
actions/setup-go — pin to commit `d35c59abb061a4a6fb18e82ac0862c26744d6ab5` (tag v5.5.0).

Go version: `1.26.2` — matches `.github/workflows/logql-correctness.yml:52,99` and CLAUDE.md.

SHA-pin format used elsewhere in this repo (see `.github/workflows/secret-scanning.yml:17`):

  uses: org/action@<40-char-sha> # vX.Y.Z

Existing workflows in this repo do NOT pin actions/checkout / actions/setup-go
to SHAs — they use `@v4`. This workflow is intentionally the first to pin,
per the task constraint.

Out of scope for this plan (explicitly deferred — do not add):
  - cmd/dataobj-inspect/ separate go.mod (follow-up plan)
  - operator/ separate go.mod (follow-up plan)
  - loki-build-image integration (user flagged for later)
  - Baseline / diff-against-main logic (want to see real finding count first)
  - Any non-govulncheck steps (no tests, no lint, no build)
</context>

<tasks>

<task type="auto">
  <name>Task 1: Create .github/workflows/govulncheck.yml</name>
  <files>.github/workflows/govulncheck.yml</files>
  <action>
Create a new GitHub Actions workflow file at `.github/workflows/govulncheck.yml`.

Required content shape (do NOT inline this verbatim if you spot a real syntactic
issue — keep semantics identical, fix syntax):

1. `name:` — `govulncheck`.

2. `on:` block — `pull_request` only, with a `paths:` filter listing:
   `**.go`, `**/go.mod`, `**/go.sum`. No `push:`, no `schedule:`, no
   `workflow_dispatch:` for this plan.

3. Top-level `permissions:` block — exactly `contents: read`. Nothing else.
   This must be at the workflow top level (not just job level) so all jobs
   inherit the minimal permission set.

4. One job, name `govulncheck`. `runs-on: ubuntu-latest`
   (do NOT use `ubuntu-x64` / `github-hosted-ubuntu-arm64-large` — keep this
   workflow on a stock runner for portability; it's CPU-light).
   `timeout-minutes: 15` (govulncheck on the Loki root module typically
   completes in a few minutes; 15 leaves headroom without holding queue slots).

5. Steps, in order:

   a. Checkout — `uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2`.
      Set `persist-credentials: false` (security hardening — no need for the
      workflow to push or auth as GITHUB_TOKEN after checkout).

   b. Setup Go — `uses: actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5 # v5.5.0`
      with `go-version: "1.26.2"` and `cache: false`. We disable the built-in
      `setup-go` cache because we manage caching explicitly in step (c) so the
      cache key is deterministic and shared across this workflow's runs.

   c. Cache module + build caches — `uses: actions/cache@v4`
      (note: cache action does NOT need SHA pinning per the task scope — only
      checkout and setup-go were called out as pinned. Use `@v4` tag here for
      consistency with the rest of the repo.) Cache `path:` should be both
      `~/go/pkg/mod` and `~/.cache/go-build` (one path per line under the
      `path:` key). `key:` must be
      `${{ runner.os }}-govulncheck-go-${{ hashFiles('go.sum') }}`.
      `restore-keys:` should fall back to `${{ runner.os }}-govulncheck-go-`.
      Use only root `go.sum` for the hash — we are explicitly out-of-scope for
      cmd/dataobj-inspect and operator submodules.

   d. Install govulncheck —
      `run: go install golang.org/x/vuln/cmd/govulncheck@latest`.
      Use `@latest` for now (the task description allows pinned-or-latest;
      `@latest` keeps us current with the vuln DB tooling and govulncheck has
      a stable CLI). The vuln database itself is always fetched fresh by the
      tool at runtime.

   e. Run govulncheck —
      `run: govulncheck ./...`.
      Working directory is the repo root (default). Do NOT pass `-test` or
      `-mode=binary` — default mode scans source and is what we want for the
      reachability signal. The step (and therefore the job) fails non-zero on
      any finding; that's intentional and is how the workflow gates the PR.

6. Do NOT add any additional steps (no `go mod download`, no `go build`, no
   tests, no upload-artifact). This workflow has exactly one assertion:
   govulncheck passes.

7. File must end with a trailing newline (standard YAML hygiene).

Implementation note — header comment: prepend the file with a short YAML
comment block (lines starting `#`) that records:
   - the purpose (PR-gating govulncheck)
   - the SHA-pin rationale (security-focused workflow sets the example)
   - the explicit out-of-scope items (dataobj-inspect submodule, operator
     submodule, loki-build-image, baseline/diff) so the next person who looks
     at this file knows what was deliberately left out.

DO NOT update `.github/workflows/logql-correctness.yml`, `secret-scanning.yml`,
or any other existing workflow as part of this plan — single-file change only.
  </action>
  <verify>
    <automated>test -f .github/workflows/govulncheck.yml && grep -q 'actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683' .github/workflows/govulncheck.yml && grep -q 'actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5' .github/workflows/govulncheck.yml && grep -q 'go-version: "1.26.2"' .github/workflows/govulncheck.yml && grep -q 'govulncheck ./...' .github/workflows/govulncheck.yml && grep -q 'contents: read' .github/workflows/govulncheck.yml && grep -E '^\s*-\s*"?\*\*\.go"?' .github/workflows/govulncheck.yml && grep -E '^\s*-\s*"?\*\*/go\.sum"?' .github/workflows/govulncheck.yml && (command -v actionlint >/dev/null 2>&1 && actionlint .github/workflows/govulncheck.yml || echo "actionlint not installed — manual YAML review required, document in SUMMARY") && (command -v govulncheck >/dev/null 2>&1 && govulncheck -version || (go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck -version))</automated>
  </verify>
  <done>
    - `.github/workflows/govulncheck.yml` exists.
    - File contains the two SHA-pinned `uses:` lines with their `# vX.Y.Z` comments exactly as specified.
    - File contains `go-version: "1.26.2"`, `govulncheck ./...`, top-level `permissions:` with `contents: read`, and the `paths:` filter covering `**.go`, `**/go.mod`, `**/go.sum`.
    - `actionlint` passes against the new file (or, if actionlint is not installed in the local env, the SUMMARY explicitly records that and notes manual YAML review was performed).
    - `govulncheck -version` runs locally (proves the install command is well-formed); we do NOT gate on findings here — finding count will be observed when the workflow first runs on a PR.
    - Commit landed on the working branch with message `ci: add govulncheck PR workflow`.
  </done>
</task>

</tasks>

<verification>
Phase-level checks (run after the task completes):

1. `cat .github/workflows/govulncheck.yml` — visually confirm structure matches
   the spec in Task 1 action: workflow `name`, `on.pull_request.paths`,
   top-level `permissions`, single `govulncheck` job, five steps in order
   (checkout, setup-go, cache, install, run).

2. `grep -c '@v[0-9]' .github/workflows/govulncheck.yml` — count of tag-pinned
   uses lines. Expected: 1 (only `actions/cache@v4` — checkout and setup-go
   are SHA-pinned, govulncheck install does not use a `uses:`). If this comes
   back >1, you've accidentally tag-pinned checkout or setup-go.

3. `grep -c '@[0-9a-f]\{40\}' .github/workflows/govulncheck.yml` — count of
   SHA-pinned uses lines. Expected: 2 (checkout + setup-go).

4. Sanity-check the paths filter does NOT include unrelated globs (e.g.
   `**.yaml`, `**.md`) — only Go-related paths trigger this workflow.
</verification>

<success_criteria>
- New file `.github/workflows/govulncheck.yml` exists in the repo.
- Workflow only triggers on PRs that touch `**.go`, `**/go.mod`, or `**/go.sum`.
- `actions/checkout` and `actions/setup-go` are SHA-pinned with `# vX.Y.Z` trailing comments.
- Go 1.26.2 is used (matches the rest of the repo).
- `govulncheck ./...` runs against the root module and the job fails if it exits non-zero.
- Top-level `permissions:` is `contents: read`.
- Cache is keyed on root `go.sum` and covers both `~/go/pkg/mod` and `~/.cache/go-build`.
- Header comment in the file records the deferred out-of-scope items so a future reader knows what's intentionally missing.
- No other files modified; commit landed.
</success_criteria>

<output>
Create `.planning/quick/260525-gch-govulncheck-pr-ci/260525-gch-SUMMARY.md` when done, recording:
  - The final file path and line count.
  - Whether `actionlint` was available and the result if so.
  - The local `govulncheck -version` output (proves the install command works).
  - Explicit list of deferred follow-ups (cmd/dataobj-inspect, operator, loki-build-image, baseline/diff) — copy from the file's header comment so it's discoverable from the SUMMARY too.
  - Note that first-PR finding count is unknown and will be observed when the workflow first runs against a real PR.
</output>
