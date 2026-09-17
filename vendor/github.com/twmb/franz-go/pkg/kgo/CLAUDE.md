# CLAUDE.md

## Package Overview

franz-go is a pure Go Kafka client library. The `kgo` package is the main client package.

## Style

- Never use non-ASCII characters in code or comments, use simple characters instead: dashes, => for arrows, etc.
- Always run `gofmt` before committing
- Internal comments should explain WHY we are doing something, not just WHAT we are doing. WHAT comments are almost never useful, unless the block that follows is complex.
- Comments around subtle race conditions (logic or data) should contain a walkthrough of how the race is encountered. Don't JUST say what the race is, but also how a sequence of events can encounter the race.

## Context keys: NEVER use empty struct as a key

Per the Go spec, two distinct zero-size variables may share the same address. That means `type myKey struct{}` is unsafe as a `context.WithValue` key - it can collide with any other package using the same pattern. Use the project's pointer-to-string idiom instead:

```go
var myKey = func() *string { s := "my_key"; return &s }()
```

The string body is for debugging; the pointer's identity is what makes the key unique. Examples in this package: `ctxPinReq` (broker.go), `noShardRetryCtx` (client.go), `commitContextFn` / `txnCommitContextFn` (consumer_group.go), `ctxRecRecycle` (pools.go).

## Commit Style

- Format: `kgo: <description>` (lowercase, no period)
- Body should explain the "why" not just the "what"
- Reference KIPs (Kafka Improvement Proposals) for protocol changes
- Include `Closes #<issue>` when fixing GitHub issues

## Protocol Behavior

The Java broker at `~/src/apache/kafka/` is the protocol's ground
truth. The Java reference client at
`clients/src/main/java/org/apache/kafka/clients/consumer/internals/` is
the ground truth for how to talk to it. KIPs are a tertiary source:
https://cwiki.apache.org/confluence/display/KAFKA/Kafka+Improvement+Proposals

When evaluating whether the broker will accept X, trace the full state
lifecycle behind X, not just the entry point: creation, destruction
(leader change `onBecomingFollower`, disconnect `onDisconnect`, member
fence, acquisition-lock timeout, session replace, cache eviction), and
rehydration (including what defaults the reloaded state uses for
transient fields). Missing a destruction or rehydration path is the
usual failure mode.

Existing conservative kgo guards are presumed correct. Removal requires
both (a) a broker trace covering creation/destruction/rehydration that
shows the broker accepts what kgo drops, and (b) the Java reference
client not having an equivalent guard.

If the user pushes back on a conclusion ("are you sure?", "isn't this
big?"), retrace the broker from a path you didn't read -- don't
restate the previous reasoning.

## Approach

Before implementing a fix for any bug, first verify whether existing code
already handles the scenario. Analyze the current codebase's safety mechanisms
(e.g., prerevoke logic, error handlers, retry paths) before writing new code.

### Audit vs. implementation

Audit requests ("review this", "find bugs", "propose a refactor") report
findings in prose with file:line citations; they do NOT call Edit/Write
on the audited code. Edits only happen on an explicit imperative for a
specific change ("apply this", "go ahead") or a direct yes to "want me
to apply this?". Passing remarks like "you do it" inside an analysis
request do not count. If a session will land many edits to one file,
ask for a standing per-file grant scoped to that file and session.

### Refactoring threshold (DRY)

DRY is about logic, not line counts. Reject helpers that exist to
differentiate callers with a `bool` flag (that's two operations with
shared infrastructure, not one operation with a knob) or that save
fewer than ~5 lines per site. Accept helpers that name a genuinely
duplicated operation, have one job, and leave the call site reading
like the operation it is performing.

## Tool use

Use `Read`, `Grep`, and `Glob` for code exploration; do not reach for
`Bash` with `cat`/`head`/`tail`/`find`/`ls`/`awk`/`sed`/`grep`. Reserve
`Bash` for tests, `gofmt`/`go vet`/`go build`, git, and gh CLI.

## Key Files

- `broker.go` - Connection handling, SASL, request/response.
- `config.go` - Client configuration options.
- `client.go` - Main client logic.
- `source.go` - Fetches from one broker. A source owns many cursors;
  each cursor tracks consume progress for one partition.
- `sink.go` - Produces to one broker. A sink owns many recBufs (one
  per partition); a recBuf owns many recBatch.
- `consumer.go` - Consumer abstraction over fetching. Validates cursor
  offsets (OffsetForLeaderEpoch for data loss, ListOffsets for
  epoch/leader). Owns sources.
- `producer.go` - Producer abstraction over producing. Picks the sink
  for each record, finishes promises, and runs cross-sink operations.
- `consumer_group.go` - Group consumer: decides which partitions to
  consume and feeds the subscription into consumer. Manages membership
  and offset commits.
- `consumer_direct.go` - Direct consumer: user-assigned partitions.
  Mostly metadata-driven topic/regex resolution.
- `txn.go` - GroupTransactSession. Bundles group logic with transaction
  logic; paranoid about aborting to prevent duplicates.
- `metadata.go` - Periodic metadata refresh; feeds producer/consumer.

## Testing

- `go test ./...` needs a local broker on port 9092. `../kfake/` is an
  in-process fake broker for unit-level tests; use a `go.work` in
  `pkg/kfake/` to test against local kgo changes. Always run
  `go test -race`.
- Unit tests should be fast. Tests >2s are suspicious even when
  passing. Only TestGroupETL / TestTxnETL should take real time
  (~2min without -race, ~3-4min with).
- When working in `pkg/kgo`, don't speculatively run kfake tests -- a
  parallel session may have kfake in a broken state. Verify with
  `go build` / `go vet`; run kfake tests only when the task is inside
  `pkg/kfake/` or explicitly asked.

## Design / concurrency.

- Refer to ../../DESIGN.md for a high level design of all the operations that can happen.
- Update the design file as necessary / when making significant changes.
