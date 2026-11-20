---
title: Capture performance profiles (pprof)
menuTitle: Capture performance profiles
description: Describes how to capture Go pprof performance profiles from Grafana Loki components to attach to bug reports.
weight: 300
---

# Capture performance profiles (pprof)

When reporting performance-related bugs, include a [Go pprof](https://pkg.go.dev/runtime/pprof) profile from the affected Loki component. Profiles make it much easier for maintainers to identify hotspots, lock contention, and memory issues.

Loki exposes the standard Go `net/http/pprof` endpoints on the HTTP port of each component. If the affected component runs on `localhost:3100`, the endpoints are available under `http://localhost:3100/debug/pprof/`.

## Before you begin

Ensure you have the following:

- Network access to the HTTP port of the affected Loki component
- [`go`](https://go.dev/doc/install) installed locally (for the `go tool pprof` client), or `curl`

## Capture a CPU profile

CPU profiles show where the component spends time. Capture over 30 seconds under the workload that reproduces the problem:

```bash
go tool pprof -top http://localhost:3100/debug/pprof/profile?seconds=30
```

Save a copy to attach to your bug report:

```bash
curl -o cpu.pb.gz "http://localhost:3100/debug/pprof/profile?seconds=30"
```

## Capture a heap profile

Heap profiles show current memory allocations and are useful for memory leaks or excessive garbage collection:

```bash
curl -o heap.pb.gz http://localhost:3100/debug/pprof/heap
```

## Capture a goroutine profile

Goroutine dumps help debug hangs, deadlocks, and stuck requests:

```bash
curl -o goroutines.txt http://localhost:3100/debug/pprof/goroutine?debug=1
```

## Capture block and mutex profiles

Block and mutex profiles show lock contention. Set non-zero sampling rates first, either with command-line flags:

```bash
-profiling.block-profile-rate=1 -profiling.mutex-profile-fraction=1
```

or in the Loki configuration file:

```yaml
profiling:
  block_profile_rate: 1
  mutex_profile_fraction: 1
```

Then capture the profiles:

```bash
curl -o block.pb.gz http://localhost:3100/debug/pprof/block
curl -o mutex.pb.gz http://localhost:3100/debug/pprof/mutex
```

## Include profiles in your bug report

Attach the saved profile files (`.pb.gz` or `.txt`) to your GitHub issue, along with:

- The Loki version or commit SHA
- The component(s) the profiles were captured from (distributor, ingester, querier, etc.)
- A short description of the workload while the profile was captured

For a visual overview you can render profiles locally with `go tool pprof -http=:8080 <profile-file>`, or use an online viewer such as <https://flamegraph.com/>.
