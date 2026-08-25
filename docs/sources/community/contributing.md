---
title: Contributing to Loki
description: Contributing to Loki
weight: 200
---

# Contributing to Loki

For the full contribution guide, see [CONTRIBUTING.md](https://github.com/grafana/loki/blob/main/CONTRIBUTING.md) in the root of the repository.

## Building

Clone the repository and use `make` to build:

```bash
git clone https://github.com/grafana/loki.git
cd loki
```

| Command | Output |
|---|---|
| `make loki` | `./cmd/loki/loki` |
| `make logcli` | `./cmd/logcli/logcli` |
| `make loki-canary` | `./cmd/loki-canary/loki-canary` |
| `make all` | all of the above |
| `make loki-image` | Docker image for Loki |

## Running tests

```bash
make test              # unit tests
make test-integration  # integration tests (requires Docker, ~15 min)
make lint              # run linters
```

## Contribute to the Helm Chart

Refer to [production/helm/loki/CONTRIBUTING.md](https://github.com/grafana/loki/blob/main/production/helm/loki/CONTRIBUTING.md) for chart-specific guidelines. The official Loki Helm chart is maintained in the [grafana/helm-charts](https://github.com/grafana/helm-charts) repository.
