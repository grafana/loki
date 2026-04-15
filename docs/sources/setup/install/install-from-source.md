---
title: Build from source
menuTitle:  Install from source
description: Describes how to install Loki from the source code.
aliases: 
  - ../../installation/install-from-source/
weight: 700
---
# Build from source

Clone the Grafana Loki repository and use the provided `Makefile`
to build Loki from source.

{{< admonition type="note" >}}
Grafana Loki does not come with any included authentication layer. You must run an authenticating reverse proxy in front of your services to prevent unauthorized access to Loki (for example, nginx). Refer to [Manage authentication](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/authentication/) for a list of open-source reverse proxies you can use.
{{< /admonition >}}

## Prerequisites

- [Go](https://golang.org/), version 1.23 or later;
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
