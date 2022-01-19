---
title: Query scenario
---

# Running the queries using xk6-loki extension

## Test scenario

To run the test, it's necessary to create JavaScript scenario. We have already created
an [example](https://github.com/grafana/xk6-loki/blob/21307675b3e2a3e0a2b907d2131c9e906a6f6d60/examples/read-scenario.js), so you can reuse and extend it.

This scenario allows to configure the ratio of each type of query and ratio of time ranges.

## Ratio

According to your needs, you can configure ratio of each type of query, ration of time ranges, etc.

To do this you need:

1. to define ratio config. The config it's just an array of items with fields:

    - `ratio` - (number) `0 > ration < 1`
    - `item` - (any)

   Example of ratio config of query types:

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

   _Note: `item` here is just a function that will be executed if it is
   selected. [See.](https://github.com/grafana/xk6-loki/blob/21307675b3e2a3e0a2b907d2131c9e906a6f6d60/examples/read-scenario.js#L139)_

   According to this config, the ratio of requests during test run will be following:

    * ~10% of labels requests
    * ~15% of label values requests
    * ~5% of requests for series
    * ~50% of range queries
    * ~20% of instant queries
    
2. to pass the config to the function `createSelectorByRatio` that will create a function that will consume random number and will choose `item` according to
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

### Instant queries [(docs)](https://github.com/grafana/xk6-loki#method-clientinstantqueryquery-limit)

Here is the function that will be run for instant query.

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
  // Assert the response from loki.
  checkResponse(res, "successful instant query");
}
```

The example scenario contains 8 instant queries that you can use, extend or replace.

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

These queries will be randomly picked for each request. Each query will randomly select label value from labels pool to use it in the query.

Also, if you want to define ratio per query, it's possible to use the same approach that was used to define ration for each query type and create query ratio
config.

### Range queries [(docs)](https://github.com/grafana/xk6-loki#method-clientrangequeryquery-duration-limit)

Here is the function that will be run for instant query.

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
  // Assert the response from loki.
  checkResponse(res, "successful range query", range);
}
```

The example scenario contains already defined queries for range query requests. It contains all `instantQuerySuppliers` + 4 additional queries.

```javascript
const rangeQuerySuppliers = [
  ...instantQuerySuppliers,
  () => `{app="${randomChoice(conf.labels.app)}"}`,
  () => `{app="${randomChoice(conf.labels.app)}"} |= "USB" != "USB"`,
  () => `{app="${randomChoice(conf.labels.app)}"} |~ "US.*(a|o)"`,
  () => `{app="${randomChoice(conf.labels.app)}", format="json"} | json | status < 300`,
]
```

In the same way as for instant queries, they will be randomly picked and each query will randomly select label value from labels pool.

### Labels query [(docs)](https://github.com/grafana/xk6-loki#method-clientlabelsqueryduration)

Here is the function that will be run for labels query.

```javascript
/**
 * Execute labels query with given client
 */
function readLabels() {
  // Randomly select the range.
  const range = selectRangeByRatio(Math.random())
  // Execute query.
  let res = client.labelsQuery(range);
  // Assert the response from loki.
  checkResponse(res, "successful labels query", range);
}
```

### Label values [(docs)](https://github.com/grafana/xk6-loki#method-clientlabelvaluesquerylabel-duration)

Here is the function that will be run for label values query.

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
  // Assert the response from loki.
  checkResponse(res, "successful label values query", range);
}
```

### Series query [(docs)](https://github.com/grafana/xk6-loki#method-clientseriesquerymatchers-duration)

Here is the function that will be run for series query.

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
  // Assert the response from loki.
  checkResponse(res, "successful series query", range);
}
```

## Labels pool

If you use xk6-loki extension to generate logs in Loki, you can use variable `conf.labels` that contains label names and values that are reproducible
generated[(See)](https://github.com/grafana/xk6-loki#labels). Pay attention that you have to use the same labels cardinality config for `write` and `read`
scenarios.

Also, you can define your own pool of label names and values and randomly select labels from this pool instead of generated pool. 

