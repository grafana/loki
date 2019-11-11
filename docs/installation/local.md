# Installing Loki Locally

## Release Binaries

Every [Loki release](https://github.com/grafana/loki/releases) includes
prebuilt binaries:

```bash
# download a binary (modify app, os, and arch as needed)
# Installs v0.4.0. Go to the releases page for the latest version
$ curl -fSL -o "/usr/local/bin/loki.gz" "https://github.com/grafana/loki/releases/download/v0.4.0/loki-linux-amd64.gz"
$ gunzip "/usr/local/bin/loki.gz"

# make sure it is executable
$ chmod a+x "/usr/local/bin/loki"
```

## Community openSUSE Linux packages

If you use openSUSE Linux you can install packaged version of Loki with:

1) Add the repository `https://download.opensuse.org/repositories/security:/logging/` to your system. 
   E.g if you are using Leap 15.1 use `sudo zypper ar https://download.opensuse.org/repositories/security:/logging/openSUSE_Leap_15.1/security:logging.repo ; sudo zypper ref`

2) Install package with `zypper in loki`

3) Enable services:

-  `systemd start loki` and `systemd enable loki`
-  `systemd start promtail` and `systemd enable promtail`

4) Adapt the configuration files:

`/etc/loki/promtail.yaml`
`/etc/loki/loki.yaml`

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
