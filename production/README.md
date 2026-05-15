# Running Loki

Currently there are six ways to try out Loki, in order from easier to hardest:

- [Grafana Cloud: Hosted Logs](#grafana-cloud-logs)
- [Run Loki locally with Docker](#run-locally-using-docker)
- [Run Loki with Nomad](#run-with-nomad)
- [Use Helm to deploy on Kubernetes](#using-helm-to-deploy-on-kubernetes)
- [Build Loki from source](#build-and-run-from-source)
- [Get inspired by our production setup](#get-inspired-by-our-production-setup)

For the various ways to send logs to Loki, see the [Send data documentation](https://grafana.com/docs/loki/latest/send-data/).

## Grafana Cloud: Hosted Logs

Grafana is offering hosted Loki as part of our broader Grafana Cloud platform. Learn more at [grafana.com/loki](https://grafana.com/oss/loki/#products-and-services).

## Run locally using Docker

The Docker images for [Loki](https://hub.docker.com/r/grafana/loki/) are available on DockerHub.

To test locally, we recommend using the `docker-compose.yaml` file in this directory. Docker starts containers for Loki and Grafana.

1. Either `git clone` this repository locally and `cd loki/production`, or download a copy of the [docker-compose.yaml](docker-compose.yaml) locally.

1. Ensure you have the most up-to-date Docker container images:

   ```bash
   docker-compose pull
   ```

1. Run the stack on your local Docker:

   ```bash
   docker-compose up
   ```

1. Grafana should now be available at http://localhost:3000/.


For instructions on how to query Loki, see [our usage docs](https://grafana.com/docs/loki/latest/logql/).

To deploy a cluster of loki locally, please refer to this [doc](./docker/)

## Run with Nomad

There are example [Nomad jobs](./nomad) that can be used to deploy Loki with
[Nomad](https://www.nomadproject.io/) - simple and powerful workload
orchestrator from HashiCorp.

## Using Helm to deploy on Kubernetes

Here is the Helm chart used to deploy Loki to Kubernetes:
- [Loki](./helm/loki/README.md#loki)

| Helm Chart version | Loki version | GEL version |
| ------------------ | ------------ | ----------- |
|      4.4.3         |    2.7.3     |    1.6.1    |
|      4.4.2         |    2.7.2     |    1.6.1    |
|      4.4.1         |    2.7.0     |    1.6.0    |
|      4.4.0         |    2.7.0     |    1.6.0    |
|      4.3.0         |    2.7.0     |    1.6.0    |
|      4.2.0         |    2.7.0     |    1.6.0    |
|      4.1.0         |    2.7.0     |    1.6.0    |
|      4.0.0         |    2.7.0     |    1.6.0    |
|      3.10.0        |    2.7.0     |    1.6.0    |
|      3.9.0         |    2.7.0     |    1.6.0    |
|      3.8.2         |    2.7.0     |    1.6.0    |
|      3.8.1         |    2.7.0     |    1.6.0    |
|      3.8.0         |    2.7.0     |    1.6.0    |
|      3.7.0         |    2.7.0     |    1.6.0    |
|      3.6.1         |    2.7.0     |    1.6.0    |
|      3.6.0         |    2.7.0     |    1.6.0    |
|      3.5.0         |    2.6.1     |    1.6.0    |

## Build and run from source

First, see the [build from source](../README.md) section of the root readme.

Grafana is Loki's UI. To query your logs you need to start Grafana as well:

```bash
$ docker run -ti -p 3000:3000 grafana/grafana:master
```

Grafana should now be available at http://localhost:3000/. Follow the [steps for configuring the datasource in Grafana](https://grafana.com/docs/loki/latest/getting-started/grafana/) and set the URL field to `http://host.docker.internal:3100`.

For instructions on how to use Loki, see [our usage docs](https://grafana.com/docs/loki/latest/logql/).

## Get inspired by our production setup

We run Loki on Kubernetes with the help of ksonnet.
You can take a look at [our production setup](ksonnet/).

To learn more about ksonnet, check out its [documentation](https://ksonnet.io).
