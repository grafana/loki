---
title: Use k6 to load test the write path
menuTitle:  Write path testing
description: Using k6 to load test the write path (ingest).
aliases: 
- ../../clients/k6/write-scenario/
weight: 920
---

# Use k6 to load test the write path

There are multiple considerations when
load testing a Loki cluster's write path.

The most important consideration is the setup of the target cluster.
Keep these items in mind when setting up your load test for the target cluster.

- Deployment mode. The cluster might be deployed as
a single-binary, as a simple scalable deployment, or as microservices

- Quantity of component instances. This aids in predicting the need
to horizontally scale the quantity of component instances.

- Resource allocation such as CPU, memory, disk, and network.
This aids in predicting the need to vertically scale the
underlying hardware.

These parameters can be adjusted in the load test:

* The quantity of distinct labels and their cardinality

    This will define how many active streams your load test will generate.
    Start with a small number of label values,
    to keep the quantity of streams small enough,
    such that it does not overwhelm your cluster.

* The batch size the client sends

    The batch size indirectly controls how many log lines per push request are
    sent. The smaller the batch size, and the larger the quantity
    of active streams you have,
    the longer it takes before chunks are flushed.
    Keeping lots of chunks
    in the ingester increases memory consumption.

* The number of virtual users (VUs)

    VUs can be used to control the amount of parallelism with which logs should
    be pushed. Every VU runs its own loop of iterations.
    Therefore, the number of VUs has the most impact on
    the generated log throughput.
    Since generating logs is CPU-intensive, there is a threshold above which
    increasing the number VUs does not result in a higher amount of log data.
    A rule of thumb is that the
    most data can be generated when the number of VUs are set to 1-1.5 times
    the quantity of CPU cores available on the k6 worker machine.
    For example,
    set the value in the range of 8 to 12 for an 8-core machine.

* The way to run k6

    k6 supports three [execution modes](https://grafana.com/docs/k6/latest/get-started/running-k6/#execution-modes) to run a test: local, distributed, and cloud.
    Whereas running your k6 load test from a single (local
    or remote) machine is easy to set up and fine for smaller Loki clusters,
    the single machine does not load test large Loki installations,
    because it cannot create the data to saturate the write path.
    For larger tests, consider [these optimizations](https://grafana.com/docs/k6/latest/testing-guides/running-large-tests/), or run them in [Grafana Cloud k6](/products/cloud/k6) or a Kubernetes cluster with the [k6 Operator](https://github.com/grafana/k6-operator).

## Metrics

The extension collects two metrics that are printed in the
[end-of-test summary](https://grafana.com/docs/k6/latest/results-output/end-of-test/) in addition to the built-in metrics.

| name | description |
| ---- | ----------- |
| `loki_client_uncompressed_bytes` | the quantity of uncompressed log data pushed to Loki, in bytes |
| `loki_client_lines` | the number of log lines pushed to Loki |

## k6 value checks

An HTTP request that successfully pushes logs to Loki
responds with status `204 No Content`.
The status code should be checked explicitly with a [k6 check](https://grafana.com/docs/k6/latest/javascript-api/k6/check/).


## Javascript example

```javascript
import { check, fail } from 'k6';
import loki from 'k6/x/loki';

/*
 * Host name with port
 * @constant {string}
 */
const HOST = "localhost:3100";

/**
 * Name of the Loki tenant
 * passed as X-Scope-OrgID header to requests.
 * If tenant is omitted, xk6-loki runs in multi-tenant mode,
 * and every VU will use its own ID.
 * @constant {string}
 */
const TENANT_ID = "my_org_id"

/**
 * URL used for push and query requests
 * Path is automatically appended by the client
 * @constant {string}
 */
const BASE_URL = `${TENANT_ID}@${HOST}`;

/**
 * Minimum amount of virtual users (VUs)
 * @constant {number}
 */
const MIN_VUS = 1

/**
 * Maximum amount of virtual users (VUs)
 * @constant {number}
 */
const MAX_VUS = 10;

/**
 * Constants for byte values
 * @constant {number}
 */
const KB = 1024;
const MB = KB * KB;

/**
 * Definition of test scenario
 */
export const options = {
  thresholds: {
    'http_req_failed': [{ threshold: 'rate<=0.01', abortOnFail: true }],
  },
  scenarios: {
    write: {
      executor: 'ramping-vus',
      exec: 'write',
      startVUs: MIN_VUS,
      stages: [
        { duration: '5m', target: MAX_VUS },
        { duration: '30m', target: MAX_VUS },
      ],
      gracefulRampDown: '1m',
    },
  },
};

const labelCardinality = {
  "app": 5,
  "namespace": 1,
};
const timeout = 10000; // 10s
const ratio = 0.9; // 90% Protobuf
const conf = new loki.Config(BASE_URL, timeout, ratio, labelCardinality);
const client = new loki.Client(conf);

/**
 * Entrypoint for write scenario
 */
export function write() {
  let streams = randomInt(4, 8);
  let res = client.pushParameterized(streams, 800 * KB, 1 * MB);
  check(res,
    {
      'successful write': (res) => {
        let success = res.status === 204;
        if (!success) console.log(res.status, res.body);
        return success;
      },
    }
  );
}

/**
 * Return a random integer between min and max including min and max
 */
function randomInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1) + min);
}
```
