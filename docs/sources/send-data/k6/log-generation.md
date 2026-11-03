---
title: Generating log data for testing
menuTitle: Log generation
description: Using k6 to generate log data for load testing.
aliases: 
- ../../clients/k6/log-generation/
weight: 910
---
# Generating log data for testing

You can use k6 to generate log data for load testing.

## Using `pushParameterized`

Push logs to Loki with `pushParameterized`.
This method generates batches of streams in a random fashion.
This method requires three arguments:

| name | description |
| ---- | ----------- |
| `streams` | number of streams per batch |
| `minSize` | minimum batch size in bytes |
| `maxSize` | maximum batch size in bytes |

**JavaScript example code fragment:**

```javascript
import loki from 'k6/x/loki';

const KB = 1024;
const MB = KB * KB;

const conf = new loki.Config("http://localhost:3100");
const client = new loki.Client(conf);

export default () => {
   client.pushParameterized(2, 500 * KB, 1 * MB);
};
```

### Argument `streams`

The first argument of the method is the desired amount of streams per batch.
Instead of using a fixed amount of streams in each call, you can randomize the
value to simulate a more realistic scenario.

**JavaScript example code fragment:**

```javascript
function randomInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1) + min);
};

export default () => {
   let streams = randomInt(2, 8);
   client.pushParameterized(streams, 500 * KB, 1 * MB);
}
```

### Arguments `minSize` and `maxSize`

The second and third argument of the method take the lower and upper bound of
the batch size. The resulting batch size is a random value between the two
arguments. This mimics the behavior of a log client, such as Grafana Alloy,
where logs are buffered and pushed once a certain batch size
is reached or after a certain size when no logs have been received.

The batch size is not equal to the payload size, as the batch size only counts
bytes of the raw logs. The payload may be compressed when Protobuf encoding
is used.

## Log format

`xk6-loki` can emit log lines in seven distinct formats. The label `format` of
a stream defines the format of its log lines.

* Apache common (`apache_common`)
* Apache combined (`apache_combined`)
* Apache error (`apache_error`)
* [BSD syslog](https://datatracker.ietf.org/doc/html/rfc3164) (`rfc3164`)
* [Syslog](https://datatracker.ietf.org/doc/html/rfc5424) (`rfc5424`)
* JSON (`json`)
* [logfmt](https://pkg.go.dev/github.com/kr/logfmt) (`logfmt`)

Under the hood, the extension uses a fork of the library
[flog](https://github.com/mingrammer/flog) for generating log lines.

## Labels

`xk6-loki` uses the following label names for generating streams:

| name      | type     | default cardinality |
| --------- | -------- | -------------------- |
| instance  | fixed    | 1 per k6 worker      |
| format    | fixed    | 7                    |
| os        | fixed    | 3                    |
| app       | variable | 5                    |
| namespace | variable | 10                   |
| pod       | variable | 50                   |
| language  | variable | not used by default  |
| word      | variable | not used by default  |

By default, the variable labels `app`, `namespace`, and `pod` are generated
automatically with the cardinalities shown above. `language` and `word` are
not used unless you specify a cardinality for them. You can override any of
these defaults using the `cardinality` argument in the `Config` constructor.

**JavaScript example code fragment:**
```javascript
import loki from 'k6/x/loki';

const cardinality = {
   "app": 1,
   "namespace": 2,
   "language": 2,
   "pod": 5,
};
const conf = new loki.Config("http://localhost:3100", 5000, 1.0, cardinality);
const client = new loki.Client(conf);
```

The total quantity of distinct streams is defined by the cartesian product of
all label values. Keep in mind that high cardinality negatively impacts the performance of
the Loki instance.

## Custom labels

Instead of the built-in label pool, you can define your own label names and
values using the `Labels` class. Pass a `Labels` instance as the fifth
argument to the `Config` constructor.

**JavaScript example code fragment:**
```javascript
import loki from 'k6/x/loki';

const labels = loki.Labels({
  "format": ["json", "logfmt"], // must contain at least one of the supported log formats
  "namespace": ["dev", "staging", "prod"],
  "container": ["nginx", "app-1", "app-2"],
});

// The fourth argument (cardinality) is not used when custom labels are set,
// but a value, such as an empty object, must still be passed.
const conf = new loki.Config("http://localhost:3100", 5000, 1.0, {}, labels);
const client = new loki.Client(conf);
```

## Payload encoding

Loki accepts two kinds of push payload encodings: JSON and Protobuf.
While JSON is easier for humans to read,
Protobuf is optimized for performance
and should be preferred when possible.

To define the ratio of Protobuf to JSON requests, the client
configuration accepts values of 0.0 to 1.0.
0.0 means 100% JSON encoding, and 1.0 means 100% Protobuf encoding.

The default value is 0.9.

**JavaScript example code fragment:**
```javascript
import loki from 'k6/x/loki';

const ratio = 0.8; // 80% Protobuf, 20% JSON
const conf = new loki.Config("http://localhost:3100", 5000, ratio);
const client = new loki.Client(conf);
```
