---
title: Loki Deployment UI 
menuTitle: Loki UI
description: Describes how to setup and use the Loki Deployment UI
weight: 100
---
# Loki Deployment UI 

{{< admonition type="warning" >}}
The Loki Deployment UI is experimental and subject to change. Before attempting to deploy the Loki make sure to consult the [Considerations](#considerations) section.
{{< /admonition >}}

The Loki Deployment UI is a web-based user interface that allows you to manage and monitor a Loki deployment in [microservice mode](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#microservices-mode) or [single binary mode](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#monolithic-mode).

{{< figure max-width="100%" src="/media/docs/loki/loki-ui.png" caption="Loki UI Home Page" >}}

## Accessing the Loki Deployment UI

The Loki Deployment UI is available at the following URL:

```
http://<LOKI_HOST>:<LOKI_PORT>/ui
```

Where `<LOKI_HOST>` is the hostname or IP address of the Loki server and `<LOKI_PORT>` is the port number that Loki is running on. By default, Loki runs on port `3100`. If you are running Loki in Kubernetes the UI is made accessible via the `loki-gateway` service. Either by port-forwarding or by exposing the service via a LoadBalancer. 

{{< admonition type="caution" >}}
`/ui` currently cannot be changed to another path. This would require further contribution.
{{< /admonition >}}

## Deployment

Each Loki component now includes the UI service. When running Loki as a docker container, the UI service is enabled by default. No additional configuration is required. 

For Loki helm users discoverability of the UI service needs to be enabled in the Loki configuration. To enable the UI discoverability, add the following paramter to the `loki` section of the `values.yaml` file:

```yaml
loki:
  ui:
   enabled: true
```

## Features

The Loki Deployment UI provides the following features to manage and monitor a Loki deployment:
| Feature | Description | Readyness |
|---------|-------------|-----------|
| [Nodes](#nodes) | Displays the status of each Loki component deployed | **Ready** |
| [Rollouts & Versions](#rollouts--versions) | A historic view of previous deployments | Coming Soon |
| [Rings](#rings) | Displays the status of the ring members | **Ready** |
| [Object Storage](#object-storage) | Displays the status of the object storage | Coming Soon |
| [Data Objects](#data-objects) | The Data Objects Explorer allows you to explore the data objects in the cluster.| Coming Soon |
| [Analyze Labels](#analyze-labels) | Analyze label distribution across your log streams | **Ready** |
| [Deletes](#deletes) | View and manage delete requests | **Ready** |
| [Limits](#limits) | View and manage tenant limits | Coming Soon |
| [Labels](#labels) | View Label stats | Coming Soon |
| [Rules](#rules) | View and manage rules | Coming Soon |


### Nodes

The Nodes page displays the status of each Loki component deployed. Depending on the deployment mode, each component will either be considered a node or a service. Below is a screenshot of the Nodes table for a microservices deployment:

{{< figure max-width="100%" src="/media/docs/loki/loki-ui-nodes.png" caption="Loki UI Nodes Table" >}}

The Node table provides the following columns:

- **Node Name**: The name of the component. This will also include any unique identifiers such as pod hashes or zone information.
- **Target**: The target is classifies the type of component the node is. This can be a distributor, ingester, querier, etc.
- **Version**: The version of the component. This is based upon the container image tag.
- **Build Date**: How old the the container image is.
- **Status**: This provides the current status of each service within a component. Each component can many services this is dependent on the type. 
- **Ready**: This provides the readiness of the component. This is based on the status of the services within the component. Services should be in a `ready` state for the component to be considered `ready`. Othetwise the component will be considered `not ready`. `Not ready` status will usually display a red icon and an error message.
- **Actions**: Take you to the Node specific page. This will allow you to view more detailed information about the component.

#### Node Page

The Node page provides detailed information about the component. The orginal information from the Nodes table is displayed at the top of the page. There will also be an extended fields useful for debugging and monitoring such as; Version, Created, Edition, Architecture, and OS.

{{< figure max-width="100%" src="/media/docs/loki/loki-ui-node-metadata.png" caption="Loki UI Node Page" >}}

Below the metadata section, there are two tools for monitoring a the specific component:
1. **Log Level**: This will allow you to change the log level of the component.
2. **Profiling Tools**: This provides the profile data for the component. These are either displayed in a new tab or downloaded as a file.

{{< figure max-width="100%" src="/media/docs/loki/loki-ui-monitoring-tools.png" caption="Loki UI Node Tools" >}}

Finally, three tabs complete the Node page:

1. **Configuration**: This provides the Loki configuration of the component. This is useful for understanding if a specific configuration has been applied to a component.
2. **Analytic**: This provides the summarized metrics for the component. This is useful for understanding the performance and status of the component. For example ingesters will display; stream count, flushed chunk rate and total, etc.
3. **Raw Metrics**: This provides the raw metrics for the component. 

### Rollouts & Versions

{{< admonition type="note" >}}

This feature is coming soon.

{{< /admonition >}}


### Rings

Rings section of the Loki UI provides a view of the status of the ring members. There can be a total of 5 rings in a Loki deployment. Each ring is responsible for a specific task. Note that not all rings will be present in a deployment, this is dependent on how you have configured your Loki deployment. For example Index Gateway and Query Scheduler both require explicit configuration to be enabled.

{{< figure max-width="100%" src="/media/docs/loki/loki-ui-ring.png" caption="Loki UI Rings Table" >}}

The Ring table provides the following columns:
* **ID**: The unique identifier for the ring member.
* **Status**: The current status of the ring member. For instance; `ACTIVE`, `UNHEALTHY`, `LEAVING`, etc.
* **Address**: The IP address of the ring member.
* **Ownership**: The percentage of the ring member's ownership of tokens.
* **Zone**: The zone the ring member is in.
* **Last Heartbeat**: The last time the ring member sent a heartbeat.

### Object Storage

{{< admonition type="note" >}}

This feature is coming soon.

{{< /admonition >}}

### Data Objects

{{< admonition type="note" >}}

This feature is coming soon.

{{< /admonition >}}

### Analyze Labels

Analyze Labels page allows you to analyze label distribution across your log streams within a given tenant and timerange. This feature is useful for understanding label cardinality. The tool can be broken down into three sections:

1. **Analyze Labels**: This provides a forum for entering your `Tenant ID`, `Time Range` and any Matchers you would like to apply. In the below example the Tennant ID is set to `meta` and the Time Range is set to `1h`. The `Matchers` field is empty. 

   {{< figure max-width="100%" src="/media/docs/loki/loki-ui-analyze-labels.png" caption="Loki UI Analyze Labels" >}}

   Note that if you are using Loki in single binary mode, or have disabled the `auth_enabled` paramater in the Loki config then the Tenant ID will be `fake`.

2. **Label Distribution**: This provides a visual representation of the label distribution. The labels are displayed in a bar chart:  

   {{< figure max-width="100%" src="/media/docs/loki/loki-ui-analyze-labels-bar.png" caption="Loki UI Analyze Labels Pie Chart" >}}

   In this example there are a total of 11 unique lables with `instance` containing the most unique values at 22. This is a label to closely monitor as it could be a high cardinality label.

3. **Label Details**: This provides a table view of the labels and a sample of their unique values. Each label can be expanded to view the unique values.

   {{< figure max-width="100%" src="/media/docs/loki/loki-ui-analyze-labels-table.png" caption="Loki UI Analyze Labels Table" >}}

   In Label Distribution example above, the `instance` label has 22 unique values. By expanding the `instance` label in the Label Details table a sample of the unique values can be viewed. There are a considerable amount of unique tokens within an instance value such as `frontend-59b8c46fcb-ft29z`. Based on the [labels best practices](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/bp-labels/) this should be moved into [Structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/).

### Deletes

The Deletes page allows you to view scheduled compactor deletes as well as manually delete data.

{{< figure max-width="100%" src="/media/docs/loki/loki-ui-deletes.png" caption="Loki UI Deletes" >}}

The Deletes table provides the following columns:
* **Status**: The status of the delete request.
* **User**: In this case this will be filled with the Tenant ID for the delete request.
* **Created At**: The time the delete request was created.
* **Range**: The time range of the delete request.
* **Deleted Lines**: The number of lines deleted.
* **Query**: The query used to delete the data.

#### New Delete Request

To create a new delete request, click the `New Delete Request` button. This will open a forum where you can enter the `Tenant ID`, `Time Range`, and `Query` for the delete request.

{{< figure max-width="100%" src="/media/docs/loki/loki-ui-new-delete-request.png" caption="Loki UI New Delete Request" >}}

### Limits

{{< admonition type="note" >}}

This feature is coming soon.

{{< /admonition >}}

### Labels

{{< admonition type="note" >}}

This feature is coming soon.

{{< /admonition >}}

### Rules

{{< admonition type="note" >}}

This feature is coming soon.

{{< /admonition >}}

## Considerations

The Loki Deployment UI is experimental and subject to change. Before attempting to deploy the Loki make sure to consult the following considerations:
1. Not all features are available in the Loki Deployment UI. Some features are still in development.
2. There is no authentication or authorization in the Loki Deployment UI. This means that anyone with access to the UI can make make delete requests, change log levels, etc.
3. This UI is not intended to replace meta monitoring for Loki.