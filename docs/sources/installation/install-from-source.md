---
title: Build from source
description: Build from source
weight: 50
---
# Build from source

Clone the Grafana Loki repository and use the provided `Makefile`
to build Loki from source.

## Prerequisites

- [Go](https://golang.org/), version 1.14 or later;
set your `$GOPATH` environment variable
- `make`
- Docker (for updating protobuf and yacc files)

## Build locally

1. Clone Loki to `$GOPATH/src/github.com/grafana/loki`:

    ```bash
    git clone https://github.com/grafana/loki $GOPATH/src/github.com/grafana/loki
    ```

2. With a current working directory of `$GOPATH/src/github.com/grafana/loki`:

    ```bash
    make loki
    ```

The built executable will be in `$GOPATH/src/github.com/grafana/loki/cmd/loki/loki`.
