# Loki UI Architecture in Distributed Mode

## Overview

Loki's UI system is designed to work seamlessly in a distributed environment. Select a node/deployment type to run the UI by including the `ui` target. This node will be the main entry point for the UI APIs, and can proxy requests to other nodes in the cluster.

## Key Components

### 1. Node Discovery and Clustering

- Each node advertises itself and maintains a list of peers (using dskit rings)
- Nodes can join and leave the cluster dynamically

### 2. UI Service Components

The UI is served by a combination of a Grafan pluing and an API Layer hosted by the Loki node running the UI target. Note all Loki pods will join the UI ring, so that the UI node can proxy requests to them when necesary.

- **Grafana Plugin**: [Loki Operational UI plugin](https://github.com/grafana/loki-operational-ui) for Grafana
- **API Layer**: REST endpoints for cluster state and proxying
- **Proxy System**: Allows forwarding requests to specific nodes
- **Service Discovery**: Tracks available nodes and their services

## Architecture Diagram

```mermaid
graph TB
    LB[Reverse Proxy /ui/]

    subgraph Cluster[Loki Cluster]
        subgraph GrafanaNode[Grafana]
            UI1[Loki Operational UI Plugin]
        end

        subgraph Node1[Node 1]
            API1[API Server]
            PROXY1[Proxy Handler]
        end

        subgraph Node2[Node 2]
            API2[API Server]
            PROXY2[Proxy Handler]
        end

        subgraph Node3[Node 3]
            API3[API Server]
            PROXY3[Proxy Handler]
        end
    end
    
    GrafanaNode --> LB
    LB --> Node1
    LB --> Node2
    LB --> Node3
```

## API Endpoints

All endpoints are prefixed with `/ui/`

### Cluster Management

- `GET /ui/api/v1/cluster/nodes`
  - Returns the state of all nodes in the cluster
  - Response includes node status, services, and build information

- `GET /ui/api/v1/cluster/nodes/{nodename}/details`
  - Returns detailed information about the specified node
  - Includes configuration, analytics, and system information

### Proxy System

- `GET /ui/api/v1/proxy/{nodename}/*`
  - Proxies requests to specific nodes in the cluster
  - Maintains original request path after the node name

### Analytics

- `GET /ui/api/v1/analytics`
  - Returns analytics data for the node

## Request Flow Examples

### Example 1: Viewing Cluster Status

1. User accesses UI via their configured Grafana instance
2. Frontend loads and makes request to `/ui/api/v1/cluster/nodes`
3. Node handling the request:
   - Queries all peers using ckit
   - Collects status from each node
   - Returns consolidated cluster state

```sequence
Grafana->Node 1: GET /ui/api/v1/cluster/nodes
Node 1->Node 2: Fetch status
Node 1->Node 3: Fetch status
Node 2-->Node 1: Status response
Node 3-->Node 1: Status response
Node 1-->Browser: Combined cluster state
```

### Example 2: Accessing Node-Specific Service

1. User requests service data from specific node
2. Frontend makes request to `/ui/api/v1/proxy/{nodename}/services`
3. Request is proxied to target node
4. Response returns directly to client

```sequence
Grafana->Node 1: GET /ui/api/v1/proxy/node2/services
Node 1->Node 2: Proxy request
Node 2-->Node 1: Service data
Node 1-->Browser: Proxied response
```

## Configuration

The UI service can be enabled by setting `ui.enabled` to `true` in the Loki config file.

```yaml
ui:
    enabled: <bool>             # Enable the UI service
```

This will enable the UI ring, and allow the UI target to serve the API. Every node must have this configured to join the ring so the API can communicate with the whole cluster, while only one node needs the specify the `ui` target. Please note enabling the UI ring via this config does not start the UI target, that must be specified separately.

## Security Considerations

1. The UI endpoints should be protected behind authentication
2. The `/ui/` prefix allows easy reverse proxy configuration
3. Node-to-node communication should be restricted to internal network

## High Availability

- Any node running the UI target can serve the UI API
- Nodes automatically discover each other
- Load balancer can distribute traffic across nodes

## Best Practices

1. Configure a reverse proxy in front of the Loki cluster
2. Use consistent node names across the cluster
4. Monitor cluster state for node health
5. Use internal network for node-to-node communication

## Concrete Example: Ring UI via Querier

### Ring UI Overview

The Ring UI is a critical component for understanding the state of Loki's distributed hash ring. Here's how it works when accessed through a querier node. Since each ingester maintains the complete ring state, querying a single ingester is sufficient to view the entire ring:

### Component Interaction

```mermaid
sequenceDiagram
    participant Grafana
    participant Querier Node
    participant Ingester

    Grafana->>Querier Node: GET /ui/api/v1/cluster/nodes
    Querier Node-->>Grafana: List of available nodes

    Note over Grafana,Querier Node: User clicks on Ring view

    Grafana->>Querier Node: GET /ui/api/v1/proxy/querier-1/ring

    Note over Querier Node: Querier fetches ring state

    Querier Node->>Ingester: Get ring status
    Ingester-->>Querier Node: Complete ring state

    Querier Node-->>Grafana: Ring state
```

### Request Flow Details

1. **Initial UI Load**

   ```http
   GET /ui/api/v1/cluster/nodes
   ```

   - Frontend loads and discovers available nodes
   - UI shows querier node in the node list

2. **Ring State Request**

   ```http
   GET /ui/api/v1/proxy/querier-1/ring
   ```

   - Frontend requests ring state through the proxy endpoint
   - Request is forwarded to the querier's ring endpoint
   - Querier gets complete ring state from a single ingester

3. **Ring Data Structure**

   ```json
   {
     "tokens": [
       {
         "token": "123456",
         "ingester": "ingester-1",
         "state": "ACTIVE",
         "timestamp": "2024-02-04T12:00:00Z"
       },
       // ... more tokens
     ],
     "ingesters": {
       "ingester-1": {
         "state": "ACTIVE",
         "tokens": ["123456", "789012"],
         "address": "ingester-1:3100",
         "last_heartbeat": "2024-02-04T12:00:00Z"
       }
       // ... more ingesters
     }
   }
   ```

### Security Notes

1. Ring access should be restricted to authorized users
2. Internal ring communication uses HTTP/2
3. Ring state contains sensitive cluster information
