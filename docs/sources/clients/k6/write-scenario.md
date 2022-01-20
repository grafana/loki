---
title: Write scenario
weight: 20
---
# Write scenario

When running a load test of the write path of a Loki cluster there are multiple
things to consider.

First, and most important is setup of the target cluster. Does the cluster run
as single-binary, simple scalable deployment, or in microservice architecture?
How many instances of each component are deployed (horizontal scaling)? How
many resources (CPU, memory, disk, network) do the individual components have
(vertical scaling)?

Depending on these parameters, you can write a load test scenario suitable for
your cluster.

The parameters that can be adjusted in the load testing scenario are the
following:

* **The amount of different labels and their cardinality.**

  This will define how many active streams your load test will generate. It's
  usually a good idea to start with only a few label values, as the amount of
  active streams otherwise might explode and may cause overwhelming your
  cluster.

* **The batch size the client is sending.**

  The batch size indirectly controls how many log lines per push request are
  sent. The smaller the batch size and the more amount of active streams you
  have, the longer it takes before chunks are flushed. Keeping lots of chunks
  around in the ingester increased memory consumption.

* **The amount of VUs.**

  VUs can be used to control the amount of parallelism with which logs should
  be pushed. Every VU runs it's own loop of iterations. This means that the
  amount of VUs has most impact on the generated log throughput.
  Since generating logs is very CPU intensive, you cannot increase the number
  indefinitely to get a higher amount of log data. A rule of thumb is that the
  most data can be generated when VUs are set 1-1.5 times the amount of CPU
  cores available on the k6 worker machine. This means on a 8 core machine you
  would set the value between 8 and 12.

* **The way to run k6.**

  k6 can be run locally, self-managed in a distributed way or highly scalable
  within the k6 cloud. Whereas running your k6 load test from a single (local
  or remote) machine is easy to setup and fine for smaller Loki clusters, it
  does not allow to load test large Loki installations, because it cannot
  create the data to saturate the write path. Therefore it makes sense to run
  the tests in the [k6 Cloud](https://k6.io/cloud/).

## Metrics

The extension collects two additional metrics that are also printed in the
[end-of-test summary](https://k6.io/docs/results-visualization/end-of-test-summary/) alongside the built-in metrics.

| name | description |
| ---- | ----------- |
| `loki_client_uncompressed_bytes` | the size of uncompressed log data pushed to Loki in bytes |
| `loki_client_lines` | the amount of log lines pushed to Loki |

## Checks

An HTTP request that pushes logs to Loki successfully responds with status `204 No Content`. The status code should be checked explicitly with a [check](https://k6.io/docs/javascript-api/k6/check-val-sets-tags/).


## Example

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
 * If tenant is omitted, xk6-loki runs in multi-tenant mode and every VU will use its own ID.
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
  let res = client.pushParametrized(streams, 800 * KB, 1 * MB);
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
