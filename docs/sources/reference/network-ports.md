---
title: Loki network ports
menuTitle: Network ports
description: Lists the ports and protocols that Loki components listen on, how the components connect to each other, and the extra ports that the Helm chart adds.
weight: 550
keywords:
  - ports
  - protocols
  - firewall
  - network policy
  - memberlist
---

# Loki network ports

Loki components communicate with each other over the network, and clients such as Grafana and Grafana Alloy reach Loki over HTTP.
Use this page to plan firewall rules, Kubernetes NetworkPolicies, or service mesh configuration for a Loki deployment.

All port numbers on this page are defaults.
If you change a port in your configuration, use your value instead.

## Ports that Loki listens on

Every Loki process opens the HTTP and gRPC ports.
Components that share [hash rings](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/hash-rings/) through memberlist also open the memberlist port.

| Port | Protocol | Configuration parameter | Purpose |
| --- | --- | --- | --- |
| 3100 | HTTP over TCP | `server.http_listen_port` | The [Loki HTTP API](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api/) for pushing and querying logs, the `/metrics` endpoint, and the `/ready` health check endpoint. |
| 9095 | gRPC over TCP | `server.grpc_listen_port` | Requests between Loki components, such as distributors sending logs to ingesters. |
| 7946 | TCP | `memberlist.bind_port` | Gossip messages that keep the hash rings in sync between components. |

Memberlist in Loki sends all gossip traffic over TCP, so you don't need to open UDP for port 7946.

If you enable TLS with `server.http_tls_config`, `server.grpc_tls_config`, or `memberlist.tls_enabled`, the encrypted traffic uses the same ports.
For details, refer to the [`server`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#server) and [`memberlist`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#memberlist) configuration blocks.

## Connections between components

The following table lists the main connections between Loki components.
In [monolithic and simple scalable modes](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/), one process runs several components, but the components still use these ports to reach each other, including across replicas.

| Source | Destination | Port | Protocol | Purpose |
| --- | --- | --- | --- | --- |
| Log collectors, such as Grafana Alloy | Distributor | 3100 | HTTP | Push logs. |
| Grafana and other query clients | Query frontend | 3100 | HTTP | Run queries. |
| Distributor | Ingester | 9095 | gRPC | Send log streams for ingestion. |
| Distributor | Pattern ingester | 9095 | gRPC | Send log lines for pattern detection, when the pattern ingester is enabled. |
| Query frontend | Query scheduler | 9095 | gRPC | Enqueue queries. |
| Query frontend | Querier | 3100 | HTTP | Proxy live tail requests, when `frontend.tail_proxy_url` is set. |
| Querier | Query scheduler | 9095 | gRPC | Pull queries to run. |
| Querier | Query frontend | 9095 | gRPC | Return query results. |
| Querier and ruler | Ingester | 9095 | gRPC | Read recent logs that aren't flushed to object storage yet. |
| Querier and ruler | Index gateway | 9095 | gRPC | Look up the index, when an index gateway is configured. |
| Querier and ruler | Compactor | 9095 or 3100 | gRPC or HTTP | Fetch delete requests, to filter deleted logs out of query results. Loki uses gRPC when `common.compactor_grpc_address` is set, and HTTP when only `common.compactor_address` is set. |
| Index gateway | Bloom gateway | 9095 | gRPC | Filter chunks with bloom filters, when bloom filters are enabled. |
| Bloom builder | Bloom planner | 9095 | gRPC | Receive bloom build tasks, when bloom filters are enabled. |
| Components that use hash rings | Each other | 7946 | TCP | Share hash ring state through memberlist. |
| Ruler | Alertmanager | Alertmanager port, usually 9093 | HTTP | Send alerts. |
| Prometheus or Grafana Alloy | All components | 3100 | HTTP | Scrape the `/metrics` endpoint. |

Loki also connects to services outside the deployment, such as object storage and caches.
Use the ports that those services expose, for example 443 for HTTPS object storage endpoints.

## Additional ports in the Helm chart

The [Loki Helm chart](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/concepts/) deploys some components that aren't part of Loki itself.
These components use the following ports:

| Component | Container port | Service port | Protocol | Helm value |
| --- | --- | --- | --- | --- |
| Gateway (NGINX) | 8080 | 80 | HTTP | `gateway.containerPort`, `gateway.service.port` |
| Chunks cache and results cache (Memcached) | 11211 | 11211 | Memcached protocol over TCP | `chunksCache.port`, `resultsCache.port` |
| Memcached exporter | 9150 | 9150 | HTTP | None |
| [Loki Canary](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/loki-canary/) | 3500 | 3500 | HTTP | None |

When the gateway is enabled, clients send push and query requests to the gateway Service on port 80.
The gateway forwards them to the Loki components on port 3100.

Some Services, such as the query frontend Service, also expose port 9096, named `grpclb`.
This port forwards to the gRPC container port, so it doesn't open another port in the pod.

The chart can create NetworkPolicies for this traffic.
To enable them, set `networkPolicy.enabled: true`.
For the related values, such as `networkPolicy.alertmanager.port`, refer to the [Helm chart values](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/reference/).
