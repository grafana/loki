---
title: Use k6 to load test log queries
menuTitle:  Query testing
description: Using k6 to load test the read path (queries).
aliases: 
- ../../clients/k6/query-scenario/
weight: 930
---
# Use k6 to load test log queries

When designing a test scenario for load testing the read path of a Loki
installation, it is important to know what types of queries you expect.

## Supported query types

Loki has 5 types of queries:

* instant query
* range query
* labels query
* label values query
* series query

In a real-world use-case, such as querying Loki using it as a Grafana
data source, all of these queries are used. Each of them has a different
[API](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api/) endpoint. The xk6-loki extension
provides a [Javascript API](https://github.com/grafana/xk6-loki#javascript-api)
for all these query types.


### Instant query

Instant queries can be executed using the function `instantQuery(query, limit)`
on the `loki.Client` instance:

**Javascript example code fragment:**

```javascript
export default () => {
  client.instantQuery(`rate({app="my-app-name"} | logfmt | level="error" [5m])`);
}
```

### Range query

Range queries can be executed using the function `rangeQuery(query, duration, limit)`
on the `loki.Client` instance:

**Javascript example code fragment:**

```javascript
export default () => {
  client.rangeQuery(`{app="my-app-name"} | logfmt | level="error"`, "15m");
}
```

### Labels query

Labels queries can be executed using the function `labelsQuery(duration)`
on the `loki.Client` instance:

**Javascript example code fragment:**

```javascript
export default () => {
  client.labelsQuery("10m");
}
```

### Label values query

Label values queries can be executed using the function `labelValuesQuery(label, duration)`
on the `loki.Client` instance:

**Javascript example code fragment:**

```javascript
export default () => {
  client.labelValuesQuery("app", "10m");
}
```

### Series query

Series queries can be executed using the function `seriesQuery(matcher, range)`
on the `loki.Client` instance:

**Javascript example code fragment:**

```javascript
export default () => {
  client.seriesQuery(`match[]={app=~"loki-.*"}`, "10m");
}
```

## Metrics

The extension collects metrics that are printed in the
[end-of-test summary](https://grafana.com/docs/k6/latest/results-output/end-of-test/) in addition to the built-in metrics.
These metrics are collected only for instant and range queries.

| name                              | description                                  |
|-----------------------------------|----------------------------------------------|
| `loki_bytes_processed_per_second` | amount of bytes processed by Loki per second |
| `loki_bytes_processed_total`      | total amount of bytes processed by Loki      |
| `loki_lines_processed_per_second` | amount of lines processed by Loki per second |
| `loki_lines_processed_total`      | total amount of lines processed by Loki      |

## Labels pool

With the xk6-loki extension, you can use the field `labels` on the `Config`
object. It contains label names and values that are generated in a reproducible
manner. Use the same labels cardinality configuration for both `write` and
`read` testing.

**Javascript example code fragment:**

```javascript
const labelCardinality = {
  "app": 5,
  "namespace": 2,
};
const conf = new loki.Config(BASE_URL, 10000, 1.0, labelCardinality);
const client = new loki.Client(conf);

function randomChoice(items) {
  return items[Math.floor(Math.random() * items.length)];
}

export default() {
  let app = randomChoice(conf.labels.app);
  let namespace = randomChoice(conf.labels.namespace);
  client.rangeQuery(`{app="${app}", namespace="${namespace}"} | logfmt | level="error"`, "15m");
}
```

Alternatively, you can define your own pool of label names and values,
and then randomly select labels from your pool instead of a generated pool. 

## Javascript example

A more complex example of a read scenario can be found in xk6-loki repository.
The test file
[read-scenario.js](https://github.com/grafana/xk6-loki/blob/main/examples/read-scenario.js)
can be resused and extended for your needs.

It allows you to configure ratios for each type of query and the ratios of time
ranges.

**Javascript example:**

```javascript
const queryTypeRatioConfig = [
  {
    ratio: 0.1,
    item: readLabels
  },
  {
    ratio: 0.15,
    item: readLabelValues
  },
  {
    ratio: 0.05,
    item: readSeries
  },
  {
    ratio: 0.5,
    item: readRange
  },
  {
    ratio: 0.2,
    item: readInstant
  },
];
```

This configuration would execute approximately

- 10% labels requests
- 15% label values requests
- 5% requests for series
- 50% range queries
- 20% instant queries

during a test run.
