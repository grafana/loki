---
title: Build from source
---
# Build from source

In order to build Loki manually, you need to clone the GitHub repo and then `make Loki`.

## Prerequisites

- Go 1.14 or later
- Make
- Docker (for updating protobuf files and yacc files)

## Build manually on your local system

Clone Loki to `$GOPATH/src/github.com/grafana/loki`:

```bash
git clone https://github.com/grafana/loki $GOPATH/src/github.com/grafana/loki
```

Then change into that directory and run `make loki`:

```bash
cd $GOPATH/src/github.com/grafana/loki
make loki
```

A file at ./cmd/loki/loki will be created and is the final built binary.
