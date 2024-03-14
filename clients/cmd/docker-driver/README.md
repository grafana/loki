# Loki Docker Logging Driver

## Overview

Docker logging driver plugins extends Docker's logging capabilities. You can use Loki Docker logging driver plugin to send
Docker container logs directly to your Loki instance or [Grafana Cloud](https://grafana.com/loki).

> Docker plugins are not yet supported on Windows; see Docker's logging driver plugin [documentation](https://docs.docker.com/engine/extend/)

If you have any questions or issues using the Docker plugin feel free to open an issue in this [repository](https://github.com/grafana/loki/issues).

The documentation source code of the plugin is available in the [documentation folder](../../docs/sources/clients/docker-driver/).

## Contributing

This directory contains the code source of the docker driver.
To build and contribute. you will need:

- Docker
- Makefile
- Optionally go 1.14 installed.

To build the driver you can use `make docker-driver`, then you can install this driver using `make docker-driver-enable`.
If you want to uninstall the driver simply run `make docker-driver-clean`.

Make sure you update the [documentation](../../docs/sources/send-data/docker-driver/) accordingly when submitting a new change.
