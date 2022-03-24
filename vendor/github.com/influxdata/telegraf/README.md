
# Telegraf

![tiger](TelegrafTiger.png "tiger")

[![Circle CI](https://circleci.com/gh/influxdata/telegraf.svg?style=svg)](https://circleci.com/gh/influxdata/telegraf) [![Docker pulls](https://img.shields.io/docker/pulls/library/telegraf.svg)](https://hub.docker.com/_/telegraf/)
[![Slack Status](https://img.shields.io/badge/slack-join_chat-white.svg?logo=slack&style=social)](https://www.influxdata.com/slack)

Telegraf is an agent for collecting, processing, aggregating, and writing metrics. Based on a
plugin system to enable developers in the community to easily add support for additional
metric collection. There are four distinct types of plugins:

1. [Input Plugins](/docs/INPUTS.md) collect metrics from the system, services, or 3rd party APIs
2. [Processor Plugins](/docs/PROCESSORS.md) transform, decorate, and/or filter metrics
3. [Aggregator Plugins](/docs/AGGREGATORS.md) create aggregate metrics (e.g. mean, min, max, quantiles, etc.)
4. [Output Plugins](/docs/OUTPUTS.md) write metrics to various destinations

New plugins are designed to be easy to contribute, pull requests are welcomed, and we work to
incorporate as many pull requests as possible. Consider looking at the
[list of external plugins](EXTERNAL_PLUGINS.md) as well.

## Minimum Requirements

Telegraf shares the same [minimum requirements][] as Go:

- Linux kernel version 2.6.23 or later
- Windows 7 or later
- FreeBSD 11.2 or later
- MacOS 10.11 El Capitan or later

[minimum requirements]: https://github.com/golang/go/wiki/MinimumRequirements#minimum-requirements

## Obtaining Telegraf

View the [changelog](/CHANGELOG.md) for the latest updates and changes by version.

### Binary Downloads

Binary downloads are available from the [InfluxData downloads](https://www.influxdata.com/downloads)
page or from each [GitHub Releases](https://github.com/influxdata/telegraf/releases) page.

### Package Repository

InfluxData also provides a package repo that contains both DEB and RPM downloads.

For deb-based platforms (e.g. Ubuntu and Debian) run the following to add the
repo key and setup a new sources.list entry:

```shell
wget -qO- https://repos.influxdata.com/influxdb.key | sudo tee /etc/apt/trusted.gpg.d/influxdb.asc >/dev/null
source /etc/os-release
echo "deb https://repos.influxdata.com/${ID} ${VERSION_CODENAME} stable" | sudo tee /etc/apt/sources.list.d/influxdb.list
sudo apt-get update && sudo apt-get install telegraf
```

For RPM-based platforms (e.g. RHEL, CentOS) use the following to create a repo
file and install telegraf:

```shell
cat <<EOF | sudo tee /etc/yum.repos.d/influxdb.repo
[influxdb]
name = InfluxDB Repository - RHEL $releasever
baseurl = https://repos.influxdata.com/rhel/\$releasever/\$basearch/stable
enabled = 1
gpgcheck = 1
gpgkey = https://repos.influxdata.com/influxdb.key
EOF
sudo yum install telegraf
```

### Build From Source

Telegraf requires Go version 1.17 or newer, the Makefile requires GNU make.

1. [Install Go](https://golang.org/doc/install) >=1.17 (1.17.2 recommended)
2. Clone the Telegraf repository:

   ```shell
   git clone https://github.com/influxdata/telegraf.git
   ```

3. Run `make` from the source directory

   ```shell
   cd telegraf
   make
   ```

### Nightly Builds

[Nightly](/docs/NIGHTLIES.md) builds are available, generated from the master branch.

### 3rd Party Builds

Builds for other platforms or package formats are provided by members of theTelegraf community.
These packages are not built, tested, or supported by the Telegraf project or InfluxData. Please
get in touch with the package author if support is needed:

- [Ansible Role](https://github.com/rossmcdonald/telegraf)
- [Chocolatey](https://chocolatey.org/packages/telegraf) by [ripclawffb](https://chocolatey.org/profiles/ripclawffb)
- [Scoop](https://github.com/ScoopInstaller/Main/blob/master/bucket/telegraf.json)
- [Snap](https://snapcraft.io/telegraf) by Laurent Sesquès (sajoupa)

## Getting Started

See usage with:

```shell
telegraf --help
```

### Generate a telegraf config file

```shell
telegraf config > telegraf.conf
```

### Generate config with only cpu input & influxdb output plugins defined

```shell
telegraf --section-filter agent:inputs:outputs --input-filter cpu --output-filter influxdb config
```

### Run a single telegraf collection, outputting metrics to stdout

```shell
telegraf --config telegraf.conf --test
```

### Run telegraf with all plugins defined in config file

```shell
telegraf --config telegraf.conf
```

### Run telegraf, enabling the cpu & memory input, and influxdb output plugins

```shell
telegraf --config telegraf.conf --input-filter cpu:mem --output-filter influxdb
```

## Documentation

[Latest Release Documentation](https://docs.influxdata.com/telegraf/latest/)

For documentation on the latest development code see the [documentation index](/docs).

- [Input Plugins](/docs/INPUTS.md)
- [Output Plugins](/docs/OUTPUTS.md)
- [Processor Plugins](/docs/PROCESSORS.md)
- [Aggregator Plugins](/docs/AGGREGATORS.md)

## Contributing

There are many ways to contribute:

- Fix and [report bugs](https://github.com/influxdata/telegraf/issues/new)
- [Improve documentation](https://github.com/influxdata/telegraf/issues?q=is%3Aopen+label%3Adocumentation)
- [Review code and feature proposals](https://github.com/influxdata/telegraf/pulls)
- Answer questions and discuss here on github and on the [Community Site](https://community.influxdata.com/)
- [Contribute plugins](CONTRIBUTING.md)
- [Contribute external plugins](docs/EXTERNAL_PLUGINS.md)
