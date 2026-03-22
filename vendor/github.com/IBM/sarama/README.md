# sarama

[![Go Reference](https://pkg.go.dev/badge/github.com/IBM/sarama.svg)](https://pkg.go.dev/github.com/IBM/sarama)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/IBM/sarama/badge?style=flat)](https://securityscorecards.dev/viewer/?uri=github.com/IBM/sarama)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/7996/badge)](https://www.bestpractices.dev/projects/7996)

Sarama is an MIT-licensed Go client library for [Apache Kafka](https://kafka.apache.org/).

## Getting started

- API documentation and examples are available via [pkg.go.dev](https://pkg.go.dev/github.com/IBM/sarama).
- Mocks for testing are available in the [mocks](./mocks) subpackage.
- The [examples](./examples) directory contains more elaborate example applications.
- The [tools](./tools) directory contains command line tools that can be useful for testing, diagnostics, and instrumentation.

You might also want to look at the [Frequently Asked Questions](https://github.com/IBM/sarama/wiki/Frequently-Asked-Questions).

## Compatibility and API stability

Sarama provides a "2 releases + 2 months" compatibility guarantee: we support
the two latest stable releases of Kafka and Go, and we provide a two month
grace period for older releases. However, older releases of Kafka are still likely to work.

Sarama follows semantic versioning and provides API stability via the standard Go
[module version numbering](https://go.dev/doc/modules/version-numbers) scheme.

A changelog is available [here](CHANGELOG.md).

## Contributing

- Get started by checking our [contribution guidelines](https://github.com/IBM/sarama/blob/main/CONTRIBUTING.md).
- Read the [Sarama wiki](https://github.com/IBM/sarama/wiki) for more technical and design details.
- The [Kafka Protocol Specification](https://cwiki.apache.org/confluence/display/KAFKA/A+Guide+To+The+Kafka+Protocol) contains a wealth of useful information.
- For more general issues, there is [a google group](https://groups.google.com/forum/#!forum/kafka-clients) for Kafka client developers.
- If you have any questions, just ask!
