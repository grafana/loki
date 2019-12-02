# Installing Loki Locally

## Release Binaries

Every release includes binaries for Loki which can be found on the
[Releases page](https://github.com/grafana/loki/releases).


## Community openSUSE Linux packages

The community provides packages of Loki for openSUSE Linux. To install:

1. Add the repository `https://download.opensuse.org/repositories/security:/logging/`
   to your system. For example, if you are using Leap 15.1, run
   `sudo zypper ar https://download.opensuse.org/repositories/security:/logging/openSUSE_Leap_15.1/security:logging.repo ; sudo zypper ref`
2. Install the Loki package with `zypper in loki`
3. Enable the Loki and Promtail services:
  - `systemd start loki && systemd enable loki`
  - `systemd start promtail && systemd enable promtail`
4. Modify the configuration files as needed: `/etc/loki/promtail.yaml` and
   `/etc/loki/loki.yaml`.

## Manual Build

### Prerequisites

- Go 1.13 or later
- Make
- Docker (for updating protobuf files and yacc files)

### Building

Clone Loki to `$GOPATH/src/github.com/grafana/loki`:

```bash
$ git clone https://github.com/grafana/loki $GOPATH/src/github.com/grafana/loki
```

Then change into that directory and run `make loki`:

```bash
$ cd $GOPATH/src/github.com/grafana/loki
$ make loki

# A file at ./cmd/loki/loki will be created and is the
# final built binary.
```
