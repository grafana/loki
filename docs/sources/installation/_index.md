---
title: Installation
weight: 200
---

# Installation

There are several methods of installing Loki and Promtail:

- [Install using Tanka (recommended)](tanka/)
- [Install using Helm](helm/)
- [Install through Docker or Docker Compose](docker/)
- [Install and run locally](local/)
- [Install from source](install-from-source/)

The [Sizing Tool](sizing/) can be used to determine the proper cluster sizing
given an expected ingestion rate and query performance. 

## General process

In order to run Loki, you must:

1. Download and install both Loki and Promtail.
1. Download config files for both programs.
1. Start Loki.
1. Update the Promtail config file to get your logs into Loki.
1. Start Promtail.
