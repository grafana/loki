---
title: Installation
menuTitle:  Install
description: Describes how to install Loki.
aliases: 
 -  ../installation/
weight: 200
---

# Installation

There are several methods of installing Loki and Promtail:

- [Install using Helm (recommended)]({{< relref "./helm" >}})
- [Install using Tanka]({{< relref "./tanka" >}})
- [Install using Docker or Docker Compose]({{< relref "./docker" >}})
- [Install and run locally]({{< relref "./local" >}})
- [Install from source]({{< relref "./install-from-source" >}})

The [Sizing Tool]({{< relref "../size/" >}}) can be used to determine the proper cluster sizing
given an expected ingestion rate and query performance.  It targets the Helm
installation on Kubernetes.

## General process

In order to run Loki, you must:

1. Download and install both Loki and Promtail.
1. Download config files for both programs.
1. Start Loki.
1. Update the Promtail config file to get your logs into Loki.
1. Start Promtail.
