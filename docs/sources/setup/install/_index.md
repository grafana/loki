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

- [Install using Helm (recommended)](helm/)
- [Install using Tanka](tanka/)
- [Install using Docker or Docker Compose](docker/)
- [Install and run locally](local/)
- [Install from source](install-from-source/)

## General process

In order to run Loki, you must:

1. Download and install both Loki and Promtail.
1. Download config files for both programs.
1. Start Loki.
1. Update the Promtail config file to get your logs into Loki.
1. Start Promtail.
