---
title: k6 Loki Extension
weight: 90
---
# k6 Loki Extension

Grafana [k6](https://k6.io) is a modern load testing tool written in Go that provides a clean and approachable scripting [API](https://k6.io/docs/javascript-api/), local and cloud execution and a flexible configuration. There are also many [extensions](https://k6.io/docs/extensions/) which add support for testing a wide range of protocols.

The [xk6-loki](https://github.com/grafana/xk6-loki) extension allows to both push logs to and query logs from a Loki instance. Thus it acts as a Loki client which can be used to simulate real-world load test scenarios on your own Loki installation.

## Installation

`xk6-loki` is an extension and not included in the standard `k6` binary. Therefore a custom `k6` binary including the extension needs to be built.

## Usage

### Configuration

### Pushing logs

### Querying logs
