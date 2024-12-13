---
title: Using k6 for load testing
menuTitle:  k6 load testing 
description: Using the k6 Loki extension for load testing.
aliases: 
- ../clients/k6/
weight:  900
---

# Using k6 for load testing

Grafana [k6](https://grafana.com/oss/k6/) is a modern load-testing tool.
Its clean and approachable scripting [API](https://grafana.com/docs/k6/latest/javascript-api/)
works locally or in the cloud.
Its configuration makes it flexible.

The [xk6-loki extension](https://github.com/grafana/xk6-loki) permits pushing logs to and querying logs from a Loki instance.
It acts as a Loki client, simulating real-world load to test the scalability,
reliability, and performance of your Loki installation.

## Before you begin

k6 is written in Golang. [Download and install](https://go.dev/doc/install) a Go environment.

## Installation

`xk6-loki` is an extension to the k6 binary.
Build a custom k6 binary that includes the `xk6-loki` extension.

1. Install the `xk6` extension bundler:

   ```bash
   go install go.k6.io/xk6/cmd/xk6@latest
   ```

1. Check out the `grafana/xk6-loki` repository:

   ```bash
   git clone https://github.com/grafana/xk6-loki
   cd xk6-loki
   ```

1. Build k6 with the extension:

   ```bash
   make k6
   ```

## Usage

Use the custom-built k6 binary in the same way as a non-custom k6 binary:

```bash
./k6 run test.js
```

`test.js` is a Javascript load test.
Refer to the [k6 documentation](https://grafana.com/docs/k6/latest/) to get started.

### Scripting API

The custom-built k6 binary provides a Javascript `loki` module.

Your Javascript load test imports the module: 

```js
import loki from 'k6/x/loki';
```

Classes of this module are:

| class | description |
| ----- | ----------- |
| `Config` | configuration for the `Client` class |
| `Client` | client for writing and reading logs from Loki |

`Config` and `Client` must be called on the k6 init context (see
[Test life cycle](https://grafana.com/docs/k6/latest/using-k6/test-lifecycle/)) outside of the
default function so the client is only configured once and shared between all
VU iterations.

The `Client` class exposes the following instance methods:

| method | description |
| ------ | ----------- |
| `push()` | shortcut for `pushParameterized(5, 800*1024, 1024*1024)` |
| `pushParameterized(streams, minSize, maxSize)` | execute push request ([POST /loki/api/v1/push](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api#ingest-logs) |
| `instantQuery(query, limit)` | execute instant query  ([GET /loki/api/v1/query](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api#query-logs-at-a-single-point-in-time) |
| `client.rangeQuery(query, duration, limit)` | execute range query  ([GET /loki/api/v1/query_range](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api#query-logs-within-a-range-of-time) |
| `client.labelsQuery(duration)` | execute labels query  ([GET /loki/api/v1/labels](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api#query-labels) |
| `client.labelValuesQuery(label, duration)` | execute label values query  ([GET /loki/api/v1/label/\<name\>/values](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api#query-label-values) |
| `client.seriesQuery(matchers, duration)` | execute series query  ([GET /loki/api/v1/series](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api#query-streams) |

**Javascript load test example:**

```js
import loki from 'k6/x/loki';

const timeout = 5000; // ms
const conf = loki.Config("http://localhost:3100", timeout);
const client = loki.Client(conf);

export default () => {
   client.pushParameterized(2, 512*1024, 1024*1024);
};
```

Refer to
[grafana/xk6-loki](https://github.com/grafana/xk6-loki#javascript-api)
for the complete `k6/x/loki` module API reference.
