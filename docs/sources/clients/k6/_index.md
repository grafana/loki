---
title: k6
weight: 90
---
# k6 Loki Extension

Grafana [k6](https://k6.io) is a modern load testing tool written in Go that provides a clean and approachable scripting [API](https://k6.io/docs/javascript-api/), local and cloud execution and a flexible configuration. There are also many [extensions](https://k6.io/docs/extensions/) which add support for testing a wide range of protocols.

The [xk6-loki](https://github.com/grafana/xk6-loki) extension allows to both push logs to and query logs from a Loki instance. Thus it acts as a Loki client which can be used to simulate real-world load test scenarios on your own Loki installation.

## Installation

`xk6-loki` is an extension and not included in the standard `k6` binary. Therefore a custom `k6` binary including the extension needs to be built.

1. Install the `xk6` extension bundler:

   ```bash
   go install go.k6.io/xk6/cmd/xk6@latest
   ```

1. Checkout the `grafana/xk6-loki` repository:

   ```bash
   git clone github.com/grafana/xk6-loki
   cd xk6-loki
   ```

1. Build `k6` with the extension:

   ```bash
   make k6
   ```

## Usage

The extended `k6` binary can be used like the regular binary:

```bash
./k6 run test.js
```

The official [k6 documentation](https://k6.io/docs/) helps you to get started with `k6`.

### Scripting API

The Loki extension provides a `loki` module that can be imported in your test file like that:

```js
import loki from 'k6/x/loki';
```

Classes of this module are:

| class | description |
| ----- | ----------- |
| `Config` | configuration for the `Client` class |
| `Client` | client for writing and reading logs from Loki |

The `Client` class exposes the following instance methods:

| method | description |
| ------ | ----------- |
| `push()` | shortcut for `pushRandomized(5, 800*1024, 1024*1024)` |
| `pushRandomized(streams, minSize, maxSize)` | execute push request ([POST /loki/api/v1/push]({{< relref "../../api/_index.md#post-lokiapiv1push" >}})) |
| `instantQuery(query, limit)` | execute instant query  ([GET /loki/api/v1/query]({{< relref "../../api/_index.md#post-lokiapiv1query" >}})) |
| `client.rangeQuery(query, duration, limit)` | execute range query  ([GET /loki/api/v1/query_range]({{< relref "../../api/_index.md#post-lokiapiv1query_range" >}})) |
| `client.labelsQuery(duration)` | execute labels query  ([GET /loki/api/v1/labels]({{< relref "../../api/_index.md#post-lokiapiv1labels" >}})) |
| `client.labelValuesQuery(label, duration)` | execute label values query  ([GET /loki/api/v1/label/<name>/values]({{< relref "../../api/_index.md#post-lokiapiv1labelnamevalues" >}})) |
| `client.seriesQuery(matchers, duration)` | execute series query  ([GET /loki/api/v1/series]({{< relref "../../api/_index.md#series" >}})) |

**Example:**

```js
import loki from 'k6/x/loki';
let timeout = 5000; // ms
let conf = loki.Config("localhost:3100", timeout);
let client = loki.Client(conf);
client.push()
```

A complete reference documentation of the Javascript API can be found on the [grafana/xk6-loki](https://github.com/grafana/xk6-loki#javascript-api) Github repository.
