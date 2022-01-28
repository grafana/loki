---
title: Query testing
weight: 30
---

# Query testing

## Javascript test example

The [example](https://github.com/grafana/xk6-loki/blob/21307675b3e2a3e0a2b907d2131c9e906a6f6d60/examples/read-scenario.js) can be resused and extended.

This environment allows the configuration of ratios for each type of query
and the ratios of time ranges.

## Ratios

Configure ratios for each type of query and the ratio of time ranges:

- Define the ratio configuration.
    It is an array of items with fields:

    - `ratio` - (number) `0 > ration < 1`
    - `item` - (any)

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

    `item` is a function that will be executed,
    as seen within the [Javascript code example](https://github.com/grafana/xk6-loki/blob/21307675b3e2a3e0a2b907d2131c9e906a6f6d60/examples/read-scenario.js#L139).

    With these example configuration settings,
    these are the ratios of requests during a test run:

    - approximately 10% of labels requests
    - approximately 15% of label values requests
    - approximately 5% of requests for series
    - approximately 50% of range queries
    - approximately 20% of instant queries
    
- This Javascript example code fragment passes configuration to
the function `createSelectorByRatio`, which will create a function
to consume a random number and will choose the `item` according to a
defined ratio.

   ```javascript
   const createSelectorByRatio = (ratioConfig) => {
     let ratioSum = 0;
     const executorsIntervals = [];
     for (let i = 0; i < ratioConfig.length; i++) {
       executorsIntervals.push({
         start: ratioSum,
         end: ratioSum + ratioConfig[i].ratio,
         item: ratioConfig[i].item,
       })
       ratioSum += ratioConfig[i].ratio
     }
     return (random) => {
       if (random >= 1 || random < 0) {
         fail(`random value must be within range [0-1)`)
       }
       const value = random * ratioSum;
       for (let i = 0; i < executorsIntervals.length; i++) {
         let currentInterval = executorsIntervals[i];
         if (value < currentInterval.end && value >= currentInterval.start) {
           return currentInterval.item
         }
       }
     }
   }
   ```

## Supported query types

The [xk6-loki Javascript API](https://github.com/grafana/xk6-loki#javascript-api) supports these query types.

### Instant queries

This function will be run for an instant query.

```javascript
/**
 * Execute instant query with given client
 */
function readInstant() {
  // Randomly select the query supplier from the pool 
  // and call the supplier that provides prepared query.
  const query = randomChoice(instantQuerySuppliers)()
  // Execute query.
  let res = client.instantQuery(query, limit);
  // Assert the response from Loki.
  checkResponse(res, "successful instant query");
}
```

This Javascript example contains 8 instant queries that you can use,
extend or replace:

```javascript
const instantQuerySuppliers = [
  () => `rate({app="${randomChoice(conf.labels.app)}"}[5m])`,
  () => `sum by (namespace) (rate({app="${randomChoice(conf.labels.app)}"} [5m]))`,
  () => `sum by (namespace) (rate({app="${randomChoice(conf.labels.app)}"} |~ ".*a" [5m]))`,
  () => `sum by (namespace) (rate({app="${randomChoice(conf.labels.app)}"} |= "USB" [5m]))`,
  () => `sum by (status) (rate({app="${randomChoice(conf.labels.app)}"} | json | __error__ = "" [5m]))`,
  () => `sum by (_client) (rate({app="${randomChoice(conf.labels.app)}"} | logfmt | __error__=""  | _client="" [5m]))`,
  () => `sum by (namespace) (sum_over_time({app="${randomChoice(conf.labels.app)}"} | json | __error__ = "" | unwrap bytes [5m]))`,
  () => `quantile_over_time(0.99, {app="${randomChoice(conf.labels.app)}"} | json | __error__ = "" | unwrap bytes [5m]) by (namespace)`,
];
```

The queries will be randomly picked for each request. Each query will randomly select a label value from the labels pool to use in the query.

To define the ratio per query, use the same approach that was used to define ratios for each query type and create the query ratio configuration.

### Range queries

This function will be run for a range query.

```javascript
/**
 * Execute range query with given client
 */
function readRange() {
  // Randomly select the query supplier from the pool 
  // and call the supplier that provides prepared query.
  const query = randomChoice(rangeQuerySuppliers)()
  // Randomly select the range.
  let range = selectRangeByRatio(Math.random());
  // Execute query.
  let res = client.rangeQuery(query, range, limit);
  // Assert the response from Loki.
  checkResponse(res, "successful range query", range);
}
```

The example contains already-defined queries for range query requests.
It contains all `instantQuerySuppliers`, plus four additional queries.

```javascript
const rangeQuerySuppliers = [
  ...instantQuerySuppliers,
  () => `{app="${randomChoice(conf.labels.app)}"}`,
  () => `{app="${randomChoice(conf.labels.app)}"} |= "USB" != "USB"`,
  () => `{app="${randomChoice(conf.labels.app)}"} |~ "US.*(a|o)"`,
  () => `{app="${randomChoice(conf.labels.app)}", format="json"} | json | status < 300`,
]
```

Like instant queries, they will be randomly picked,
and each query will randomly select a label value from the labels pool.

### Labels query

This function will be run for a labels query.

```javascript
/**
 * Execute labels query with given client
 */
function readLabels() {
  // Randomly select the range.
  const range = selectRangeByRatio(Math.random())
  // Execute query.
  let res = client.labelsQuery(range);
  // Assert the response from Loki.
  checkResponse(res, "successful labels query", range);
}
```

### Label values

This function will be run for a label values query.

```javascript
/**
 * Execute label values query with given client
 */
function readLabelValues() {
  // Randomly select label name from pull of the labels.
  const label = randomChoice(Object.keys(conf.labels));
  // Randomly select the range.
  const range = selectRangeByRatio(Math.random());
  // Execute query.
  let res = client.labelValuesQuery(label, range);
  // Assert the response from Loki.
  checkResponse(res, "successful label values query", range);
}
```

### Series query

This function will be run for a series query.

```javascript
/**
 * Execute series query with given client
 */
function readSeries() {
  // Randomly select the range.
  let range = selectRangeByRatio(Math.random());
  // Randomly select the series selector from the pool of selectors.
  let selector = randomChoice(seriesSelectorSuppliers)();
  // Execute query.
  let res = client.seriesQuery(selector, range);
  // Assert the response from Loki.
  checkResponse(res, "successful series query", range);
}
```

## Labels pool

With the xk6-loki extension,
you can use variable `conf.labels`.
It contains label names and values that are
generated in a reproducible manner.
Use the same labels cardinality configuration for both `write` and `read`
testing.

You can define your own pool of label names and values,
and then randomly select labels from your pool instead of a generated pool. 
