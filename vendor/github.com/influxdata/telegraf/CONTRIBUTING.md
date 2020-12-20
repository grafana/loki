### Contributing

1. [Sign the CLA][cla].
1. Open a [new issue][] to discuss the changes you would like to make.  This is
   not strictly required but it may help reduce the amount of rework you need
   to do later.
1. Make changes or write plugin using the guidelines in the following
   documents:
   - [Input Plugins][inputs]
   - [Processor Plugins][processors]
   - [Aggregator Plugins][aggregators]
   - [Output Plugins][outputs]
1. Ensure you have added proper unit tests and documentation.
1. Open a new [pull request][].

#### Contributing an External Plugin *(experimental)*
Input, output, and processor plugins written for internal Telegraf can be run as externally-compiled plugins through the [Execd Input](plugins/inputs/execd), [Execd Output](/plugins/inputs/execd), and [Execd Processor](plugins/processors/execd) Plugins without having to change the plugin code.

Follow the guidelines of how to integrate your plugin with the [Execd Go Shim](/plugins/common/shim) to easily compile it as a separate app and run it with the respective `execd` plugin. 
Check out our [guidelines](docs/EXTERNAL_PLUGINS.md#external-plugin-guidelines) on how to build and set up your external plugins to run with `execd`.


#### Security Vulnerability Reporting
InfluxData takes security and our users' trust very seriously. If you believe you have found a security issue in any of our
open source projects, please responsibly disclose it by contacting security@influxdata.com. More details about 
security vulnerability reporting, 
including our GPG key, [can be found here](https://www.influxdata.com/how-to-report-security-vulnerabilities/).

### GoDoc

Public interfaces for inputs, outputs, processors, aggregators, metrics,
and the accumulator can be found in the GoDoc:

[![GoDoc](https://godoc.org/github.com/influxdata/telegraf?status.svg)](https://godoc.org/github.com/influxdata/telegraf)

### Common development tasks

**Adding a dependency:**

Telegraf uses Go modules. Assuming you can already build the project, run this in the telegraf directory:

1. `go get github.com/[dependency]/[new-package]`

**Unit Tests:**

Before opening a pull request you should run the linter checks and
the short tests.

```
make check
make test
```

**Execute integration tests:**

(Optional)

Running the integration tests requires several docker containers to be
running.  You can start the containers with:
```
docker-compose up
```

And run the full test suite with:
```
make test-all
```

Use `make docker-kill` to stop the containers.


[cla]: https://www.influxdata.com/legal/cla/
[new issue]: https://github.com/influxdata/telegraf/issues/new/choose
[pull request]: https://github.com/influxdata/telegraf/compare
[inputs]: /docs/INPUTS.md
[processors]: /docs/PROCESSORS.md
[aggregators]: /docs/AGGREGATORS.md
[outputs]: /docs/OUTPUTS.md
