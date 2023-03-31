---
title: Local
description: Install and run Grafana Loki locally
weight: 40
---
# Local

In order to log events with Grafana Loki, download and install both Promtail and Loki.
- Loki is the logging engine.
- Promtail sends logs to Loki.

The configuration specifies running Loki as a single binary.

## Install using APT or RPM package manager

1. Add Granafa's Advanced Package Tool [APT](https://apt.grafana.com/) or RPM Package Manager [RPM](https://rpm.grafana.com/)
   package repository following the linked instructions.
1. Install Loki and Promtail
   1. Using `dnf`
      ```
      dnf update
      dnf install loki promtail
      ```
   1. Using `apt-get`
      ```
      apt-get update
      apt-get install loki promtail
      ```

## Install manually
1. Navigate to the [release page](https://github.com/grafana/loki/releases/).
2. Scroll down to the Assets section under the version that you want to install.
3. Download the Loki and Promtail .zip files that correspond to your system.
   **Note:** Do not download LogCLI or Loki Canary at this time. `LogCLI` allows you to run Loki queries in a command line interface. [Loki Canary]({{<relref "../operations/loki-canary">}}) is a tool to audit Loki performance.
4. Unzip the package contents into the same directory. This is where the two programs will run.
5. In the command line, change directory (`cd` on most systems) to the directory with Loki and Promtail. Copy and paste the commands below into your command line to download generic configuration files.
   **Note:** Use the corresponding Git refs that match your downloaded Loki version to get the correct configuration file. For example, if you are using Loki version 2.6.1, you need to use the `https://raw.githubusercontent.com/grafana/loki/v2.6.1/cmd/loki/loki-local-config.yaml` URL to download the configuration file that corresponds to the Loki version you aim to run.

    ```
    wget https://raw.githubusercontent.com/grafana/loki/main/cmd/loki/loki-local-config.yaml
    wget https://raw.githubusercontent.com/grafana/loki/main/clients/cmd/promtail/promtail-local-config.yaml
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

The next step will be running an agent to send logs to Loki.
To do so with Promtail, refer to the [Promtal configuration]({{<relref "../clients/promtail">}}).

## Release binaries - openSUSE Linux only

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
