---
title: Local
---
# Install and run Loki locally

In order to log events with Loki, you must download and install both Promtail and Loki.
- Loki is the logging engine.
- Promtail sends logs to Loki.

## Install and run

1. Navigate to the [release page](https://github.com/grafana/loki/releases/).
2. Scroll down to the Assets section under the version that you want to install.
3. Download the Loki and Promtail .zip files that correspond to your system.
   **Note:** Do not download LogCLI or Loki Canary at this time. [LogCLI](../../getting-started/logcli/) allows you to run Loki queries in a command line interface. [Loki Canary](../../operations/loki-canary/) is a tool to audit Loki performance.
4. Unzip the package contents into the same directory. This is where the two programs will run.
5. In the command line, change directory (`cd` on most systems) to the directory with Loki and Promtail. Copy and paste the commands below into your command line to download generic configuration files:
```
wget https://raw.githubusercontent.com/grafana/loki/master/cmd/loki/loki-local-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/master/cmd/promtail/promtail-local-config.yaml
```
6. Enter the following command to start Loki:

**Windows**

```
.\loki-windows-amd64.exe --config.file=loki-local-config.yaml
```

**Linux**
```
./loki-linux-amd64 -config.file=loki-local-config.yaml
```

Loki runs and displays Loki logs in your command line and on http://localhost:3100/metrics.

Congratulations, Loki is installed and running! Next, you might want edit the Promtail config file to [get logs into Loki](../../getting-started/get-logs-into-loki/).

## Release binaries - openSUSE Linux only

Every release includes binaries for Loki which can be found on the
[Releases page](https://github.com/grafana/loki/releases).

## Community openSUSE Linux packages

The community provides packages of Loki for openSUSE Linux. To install:

1. Add the repository `https://download.opensuse.org/repositories/security:/logging/`
   to your system. For example, if you are using Leap 15.1, run
   `sudo zypper ar https://download.opensuse.org/repositories/security:/logging/openSUSE_Leap_15.1/security:logging.repo ; sudo zypper ref`
1. Install the Loki package with `zypper in loki`
1. Enable the Loki and Promtail services:
  - `systemd start loki && systemd enable loki`
  - `systemd start promtail && systemd enable promtail`
1. Modify the configuration files as needed: `/etc/loki/promtail.yaml` and
   `/etc/loki/loki.yaml`.
