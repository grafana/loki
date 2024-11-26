---
title: LogCLI tutorial
menuTitle: LogCLI tutorial
description: Learn how to use LogCLI to query logs in Grafana Loki.
weight: 250
killercoda:
  title: LogCLI tutorial
  description: Learn how to use LogCLI to query logs in Grafana Loki.
  backend:
    imageid: ubuntu
---

# LogCLI tutorial

This [LogCLI](https://grafana.com/docs/loki/<VERSION>/query/logcli/) tutorial will walk you through the following concepts:

* Setup
* Querying logs
* Statistic Queries against your Loki instance
* Queries against static log files

## Prerequisites

Before you begin, you need to have the following:
* Docker
* Docker-compose
* LogCLI installed on your machine (see [LogCLI installation](https://grafana.com/docs/loki/<VERSION>/query/logcli/#installation))

{{< admonition type="tip" >}}
Alternatively, you can try out this example in our interactive learning environment: [LogCLI Tutorial](https://killercoda.com/grafana-labs/course/loki/logcli-tutorial).

It's a fully configured environment with all the dependencies already installed.

![Interactive](/media/docs/loki/loki-ile.svg)

Provide feedback, report bugs, and raise issues in the [Grafana Killercoda repository](https://github.com/grafana/killercoda).
{{< /admonition >}}

## Scenario

You are a site manager for a new logistics company. The company uses structured logs to keep track of every shipment sent and received. The payload format looks like this:

```json
{"timestamp": "2024-11-22T13:22:56.377884", "state": "New York", "city": "Buffalo", "package_id": "PKG34245", "package_type": "Documents", "package_size": "Medium", "package_status": "error", "note": "Out for delivery", "sender": {"name": "Sender27", "address": "144 Elm St, Buffalo, New York"}, "receiver": {"name": "Receiver4", "address": "260 Cedar Blvd, New York City, New York"}}
```

The logs are processed from Grafana Alloy to extract labels and structured metadata before they're stored in Loki. You have been tasked with monitoring the logs using the LokiCLI and build a report on the overall health of the shipments.

## Setup

To get started, we need to clone the [Alloy Scenario](https://github.com/grafana/alloy-scenarios) repository and spin up the mail-house example:

1. Clone the repository:
  ```bash
  git clone https://github.com/grafana/alloy-scenarios.git
  ```
1. Spin up the mail-house example:
  ```bash
  docker-compose -f alloy-scenarios/mail-house/docker-compose.yml up -d
  ```

This will start the mail-house example and expose the Loki instance on [`http://localhost:3100`](http://localhost:3100). We have also included a Grafana instance to verify the LogCLI results which can be accessed on [`http://localhost:3000`](http://localhost:3000).

### Connecting LogCLI to Loki

To connect LogCLI to the Loki instance, you need to set the `LOKI_ADDR` environment variable:

{{< admonition type="tip" >}}

If you are running this example against your own Loki instance and have configured authentication, you will need to set the `LOKI_USERNAME` and `LOKI_PASSWORD` environment variables as well.

{{< /admonition >}}

```bash
export LOKI_ADDR=http://localhost:3100
```

Lets now verify the connection by running the following command:

```bash
logcli labels
```

This should return an output similar to the following:

```console
http://localhost:3100/loki/api/v1/labels?end=1732282703894072000&start=1732279103894072000
package_size
service_name
state
```

This confirms that LogCLI is connected to the Loki instance and we now know that the logs contain the following labels: `package_size`, `service_name`, and `state`. Lets now run some queries against Loki to better understand our package logistics.

## Querying logs

As part of our role within the logistics company, we need to build a report on the overall health of the shipments. Unfortunately, we only have access to a console and cannot use Grafana to visualize the data. We can use LogCLI to query the logs and build the report.

### Find all critical packages

To find all critical packages in the last hour, we can run the following query:

```bash
logcli query '{service_name="Delivery World"} | package_status="critical"'
```

This will return all logs where the `service_name` is `Delivery World` and the `package_status` is `critical`. The output will look similar to the following:

```console
http://localhost:3100/loki/api/v1/query_range?direction=BACKWARD&end=1732617594381712000&limit=30&query=%7Bservice_name%3D%22Delivery+World%22%7D+%7C+package_status%3D%22critical%22&start=1732613994381712000
Common labels: {package_status="critical", service_name="Delivery World"}
2024-11-26T10:39:52Z {package_id="PKG79755", package_size="Small", state="Texas"}       {"timestamp": "2024-11-26T10:39:52.521602Z", "state": "Texas", "city": "Dallas", "package_id": "PKG79755", "package_type": "Clothing", "package_size": "Small", "package_status": "critical", "note": "In transit", "sender": {"name": "Sender38", "address": "906 Maple Ave, Dallas, Texas"}, "receiver": {"name": "Receiver41", "address": "455 Pine Rd, Dallas, Texas"}}
2024-11-26T10:39:50Z {package_id="PKG34018", package_size="Large", state="Illinois"}    {"timestamp": "2024-11-26T10:39:50.510841Z", "state": "Illinois", "city": "Chicago", "package_id": "PKG34018", "package_type": "Clothing", "package_size": "Large", "package_status": "critical", "note": "Delayed due to weather", "sender": {"name": "Sender22", "address": "758 Elm St, Chicago, Illinois"}, "receiver": {"name": "Receiver10", "address": "441 Cedar Blvd, Naperville, Illinois"}}
```

Lets suppose we want to look back for the last 24 hours, we can use the `--since` flag to specify the time range:

```bash
logcli query --since 24h '{service_name="Delivery World"} | package_status="critical"' 
```

This will query all logs for the `package_status` `critical` in the last 24 hours. However it will not return all of the logs, but only the first 30 logs. We can use the `--limit` flag to specify the number of logs to return:

```bash
logcli query --since 24h --limit 100 '{service_name="Delivery World"} | package_status="critical"' 
```

### Writing query results to a file

Another useful feature of LogCLI is the ability to write the query results to a file. This can be useful for offloading the results of our inventory report.

```bash
  logcli query \
     --timezone=UTC \
     --output=jsonl \
     --parallel-duration="5m" \
     --parallel-max-workers="4" \
     --part-path-prefix="./inventory/inv" \
     --since=24h \
     --overwrite-completed-parts \
     '{service_name="Delivery World"}'
```











