---
title: Grafana Loki configuration parameters
menuTitle: Configure
description: Configuration reference for the parameters used to configure Grafana Loki.
aliases:
  - ./configuration # /docs/loki/<LOKI_VERSION>/configuration/
weight: 400
---

# Grafana Loki configuration parameters

Grafana Loki is configured in a YAML file (usually referred to as `loki.yaml` )
which contains information on the Loki server and its individual components,
depending on which mode Loki is launched in.

Configuration examples can be found in the [Configuration Examples]({{< relref "./examples/configuration-examples" >}}) document.

<!-- The shared `configuration.md` file is generated from `/docs/templates/configuration.template`. To make changes to the included content, modify the template file and run `make doc` from root directory to regenerate the shared file. -->

{{< docs/shared lookup="configuration.md" source="loki" version="<LOKI_VERSION>" >}}
