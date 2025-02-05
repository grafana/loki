# ckit

[![Go Reference](https://pkg.go.dev/badge/github.com/grafana/ckit.svg)](https://pkg.go.dev/github.com/grafana/ckit)

ckit (clustering toolkit) is a lightweight package for creating clusters that
use [consistent hashing][] for workload distribution.

ckit works by gossiping member state over HTTP/2, and locally generating
hashing algorithms based on the state of nodes in the cluster. Because gossip
is used, the hashing algorithms are eventually consistent as cluster state
takes time to converge.

> **NOTE**: ckit is still in development; breaking changes to the API may
> happen.

[consistent hashing]: https://en.wikipedia.org/wiki/Consistent_hashing

## Features

* Low-overhead: on a 151 node cluster, ckit uses ~20MB of memory and ~50Bps of
  network traffic per node.

* HTTP/2 transport: nodes communicate over plain HTTP/2 without needing to open
  up extra ports.

## Packages

ckit has two main packages:

* The top-level package handles establishing a cluster.
* The `shard` package handles creating consistent hashing algorithms based on
  cluster state.

There are also some utility packages:

* The `advertise` package contains utilities for a node to determine what IP
  address to advertise to its peers.
* The `memconn` package contains utilities for a node to create an in-memory
  connection to itself without using the network.

## Comparison to grafana/dskit

[grafana/dskit][dskit] is a mature library with utilities for building
distributed systems in general. Its clustering mechanism works by gossiping a
32-bit hash ring over the network. In comparison, ckit locally computes 64-bit
hash rings.

dskit was built for Grafana Labs' time-series databases, while ckit was
initially built for Grafana Agent, with the intent of building something with
less operational overhead.

Compared to ckit, the dskit library:

* Is more mature, and is used at scale with Grafana Mimir, Grafana Loki, and
  Grafana Tempo.

* Gossips hash rings over the network, leading to more complexity and more
  network overhead.

* Uses a 32-bit hash ring for distributing work; ckit has multiple 64-bit
  hashing algorithms to choose from.

* Requires a separate listener for gossip traffic; ckit allows reusing your
  existing HTTP/2-capable server.

[dskit]: https://github.com/grafana/dskit
