---
title: Log generation
weight: 10
---
# Log generation

When pushing logs using the method `pushParametrized()` the Loki extension will
generate batches of streams in a random fashion. This function takes three
mandatory arguments:

| name | description |
| ---- | ----------- |
| `streams` | number of streams per batch |
| `minSize` | minimum batch size in bytes |
| `maxSize` | maximum batch size in bytes |

**Example:**

```javascript
import loki from 'k6/x/loki';

const KB = 1024;
const MB = KB * KB;

const conf = loki.Config("http://localhost:3100");
const client = loki.Client(conf);

export default () => {
   client.pushParametrized(2, 500 * KB, 1 * MB);
};
```

## Streams

The first argument of the method is the desired amount of streams per batch.
Instead of using a fixed amount of streams in each call, you can randomize the
value to simulate a more realistic scenario. The amount of streams per batch
should usually not be larger than 10.

```javascript
function randomInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1) + min);
};

export default () => {
   let streams = randomInt(2, 8);
   client.pushParametrized(streams, 500 * KB, 1 * MB);
}
```

## Batches

The second and third argument of the method take the lower and upper bound of
the batch size. The resulting batch size is a random value between the two
arguments. This mimics the behaviour of a log client, such as Promtail or
the Grafana Agent, where logs are buffered and pushed one a certain batch size
is reached of after a certain size no logs have been received.

The batch size is not equal to the payload size, as the batch size only counts
the bytes of the raw logs. The payload may be compressed when Protobuf encoding
is used.

## Log format

`xk6-loki` emits log lines in 6 different formats. The label `format` of a stream defines the format of its log lines.

* Apache common (`apache_common`)
* Apache combined (`apache_combined`)
* Apache error (`apache_error`)
* [BSD syslog](https://datatracker.ietf.org/doc/html/rfc3164) (`rfc3164`)
* [Syslog](https://datatracker.ietf.org/doc/html/rfc5424) (`rfc5424`)
* JSON (`json`)

Under the hood, the extension uses the library [flog](https://github.com/mingrammer/flog) for generating log lines.

## Labels

`xk6-loki` uses the following label names for generating streams:

| name      | type     | cardinality     |
| --------- | -------- | --------------: |
| instance  | fixed    | 1 per k6 worker |
| format    | fixed    | 6               |
| os        | fixed    | 3               |
| namespace | variable | >100            |
| app       | variable | >100            |
| pod       | variable | >100            |
| language  | variable | >100            |
| word      | variable | >100            |

By default, variable labels are not used, however, you can specify the
cardinality (amount of different label values) using the `cardinality` argument
in the `Config` constructor.

```javascript
import loki from 'k6/x/loki';

const cardinality = {
   "app": 1,
   "namespace": 2,
   "language": 2,
   "pod": 5,
};
const conf = loki.Config("http://localhost:3100", 5000, 1.0, cardinality);
const client = loki.Client(conf);
```

The total amount of different streams is defined by the carthesian product of
all label values. Keep in mind that high cardinality impacts the performance of
the Loki instance.

## Payload encoding

Loki accepts two kinds of push payload encodings: JSON and Protobuf. While the
former is easier for humans to read, the latter is optimized for performance
and should almost always be used.

To define the ratio between JSON and Protobuf requests, the client
configuration accepts a value between 0 and 1, where `0.0` means 100% JSON and
`1.0` means 100% Protobuf encoding.

The default value is `0.9`.

```javascript
import loki from 'k6/x/loki';

const ratio = 0.8; // 80% Protobuf, 20 % JSON
const conf = loki.Config("http://localhost:3100", 5000, ratio);
const client = loki.Client(conf);
```
