# Loki Docker-Compose

This folder contains the necessary to run the Loki distributed setup locally on docker-compose.

It runs the current code base in the repository this means you can debug new features and iterate very quickly by simply restarting the compose project.

To start the stack simply run:

```bash
./tools/dev/loki-tsdb-storage-s3/compose-up.sh
```

You can then access grafana locally with http://localhost:3000 (default account admin/admin). The grafana container should already have the datasource to correctly query the frontend.

To tear it down use:

```bash
./tools/dev/loki-tsdb-storage-s3/compose-down.sh
```

> On MacOS :apple: docker can get stuck when restarting the stack, you can restart docker to workaround the problem :shrug:

## Configuration

The configuration of the stack is in the [`config/`](./config/) folder.
The stack currently runs TSDB storage on Silo (a MinIO fork, via the S3 compatible API). All containers data are stored under `.data-.*` if you need it for debugging purpose.

Logs from all containers are ingested via the [loki docker driver](https://grafana.com/docs/loki/latest/clients/docker-driver/).

## Write path

The write path runs through Kafka rather than straight from distributor to ingester:

- `distributor` has `kafka_writes_enabled: true` and `ingester_writes_enabled: false`, so a push is produced to the `loki` topic and acknowledged from there.
- each ingester has `kafka_ingestion.enabled: true` and consumes the partition matching the number on the end of its hostname, so `ingester-1` owns partition 1 and `ingester-2` owns partition 2. The topic is created with four partitions, so 0 and 3 sit idle until you add an `ingester-0`/`ingester-3`.
- the partition ring lives in the same Consul as the other rings (`ingester.kafka_ingestion.partition_ring`), and `querier` sets `query_partition_ingesters: true` so reads go to the partition owners.

The broker is [AutoMQ](https://github.com/AutoMQ/automq), a Kafka-compatible broker that keeps its WAL and segments in object storage instead of on local disk. It points at the same Silo instance Loki uses, in its own `automq-data` and `automq-ops` buckets, so the whole stack — chunks, index and the Kafka log — sits on one object store. Only the KRaft metadata log is local, in the `automq-kraft` volume.

The `kafka-init` container creates the `loki` topic with a fixed partition count before Loki starts, which is why `auto_create_topic_enabled` is off in the config. Poke around the topic with [kafka-ui](http://localhost:8080), or talk to the broker from the host on `localhost:9092`:

```bash
kcat -b localhost:9092 -L
kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group ingester-1
```

## Metrics

`alloy` scrapes AutoMQ, Mimir and every Loki component, and remote-writes to a single-node `mimir` whose blocks also live in Silo (`mimir-blocks`, `mimir-ruler` and `mimir-alertmanager` buckets). Grafana gets `Mimir` as its default data source, so the metrics and the logs they describe sit side by side.

AutoMQ's metrics come from its built-in Prometheus exporter, turned on with `s3.telemetry.metrics.exporter.uri`. It labels its own series with the cluster ID as `job` and the node ID as `instance`, so the scrape sets `honor_labels = true` to leave them be — [AutoMQ's `Cluster Overview` dashboard](https://github.com/AutoMQ/automq/blob/main/docker/telemetry/grafana/provisioning/dashboards/User/cluster.json) (Apache-2.0, vendored into [`config/dashboards/`](./config/dashboards/)) keys its cluster picker off that `job` label. For more detail, add `--override s3.telemetry.metrics.level=DEBUG` to the broker, which turns on per-stage latency for writes to the object store.

Mimir runs with `multitenancy_enabled: false`, so you can curl it without a tenant header:

```bash
curl -s --get http://localhost:9009/prometheus/api/v1/query --data-urlencode 'query=kafka_broker_active_count'
```

Nothing needs to reach Alloy, so its port isn't published — scrape health lands in Mimir as `up` and `scrape_samples_scraped` anyway. Add a `12345:12345` mapping to the service if you want its UI, which lists the scrape targets and their last error.

## Ports

Every service is published on its natural port, so if you already run another stack that wants one of these, expect a fight over the binding.

| Service           | HTTP | dlv   |
| ----------------- | ---- | ----- |
| `distributor`     | 8001 | 18001 |
| `ingester-1`      | 8002 | 18002 |
| `ingester-2`      | 8003 | 18003 |
| `querier`         | 8004 | 18004 |
| `compactor`       | 8006 | 18006 |
| `query-frontend`  | 8007 | 18007 |
| `index-gateway`   | 8008 | 18008 |
| `query-scheduler` | 8009 | 18009 |

| Service    | Port        | What                  |
| ---------- | ----------- | --------------------- |
| `grafana`  | 3000        | UI                    |
| `kafka-ui` | 8080        | UI                    |
| `consul`   | 8500        | UI and API            |
| `automq`   | 9092        | Kafka `HOST` listener |
| `silo`     | 9000 / 9001 | S3 API / web console  |
| `mimir`    | 9009        | HTTP and query API    |
| `jaeger`   | 16686       | UI                    |

## Remote Debugging

You can also remote debug all Loki containers which are built in debug mode. Each container automatically runs [`dlv`](https://github.com/go-delve/delve) headless and then start Loki process.
You'll need to grab the `dlv` port from the table above for the container you want to debug. (a different one is used per container.)

### dlv

For example, if command line tooling is your thing, you can debug `ingester-1` with dlv by simply running:

```bash
dlv connect 127.0.0.1:18002
```

### vs-code

If you use vs-code, you can add this snippet bellow in your [`launch.json`](https://code.visualstudio.com/docs/editor/debugging) file:

```json
{
    "name": "Launch Loki remote",
    "type": "go",
    "request": "attach",
    "mode": "remote",
    "substitutePath": [
        {
            "from": "${workspaceFolder}",
            "to": "${workspaceFolder}"
        }
    ],
    "port": 18002,
    "host": "127.0.0.1",
    "cwd": "${workspaceFolder}/tools/dev/loki-tsdb-storage-s3/loki",
    "remotePath": "/loki/loki",
    "showLog": true,
    "trace": "log",
    "logOutput": "rpc"
}
```

Then you can debug `ingester-1` with the `Launch Loki remote` configuration within the debugging tab.

### GoLand IDE

If you use the [GoLand](https://www.jetbrains.com/go/) IDE, just create a Go remote debug configuration and use the appropriate port with the process you wish to debug.
