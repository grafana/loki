# Install Loki

## Installation methods

Instructions for different methods of installing Loki and Promtail.

- [Install using Tanka (recommended)](./tanka.md)
- [Install through Helm](./helm.md)
- [Install through Docker or Docker Compose](./docker.md)
- [Install and run locally](./local.md)
- [Install from source](./install-from-source.md)

## General process

In order to run Loki, you must:

1. Download and install both Loki and Promtail.
1. Download config files for both programs.
1. Start Loki.
1. Update the Promtail config file to get your logs into Loki.
1. Start Promtail.
