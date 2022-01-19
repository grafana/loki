---
title: Memberlist
weight: 50
---

## Overview

`Memberlist` is the default key-value store supported by Loki. For more on the function of key-value stores in Loki, please see [Consistent Hash Rings](rings.md). 

This page will provide a little more detail on memberlist and how to configure it in Loki. 

## What is memberlist?

Memberlist is a key-value store that works in a peculiar way: each component has its own copy of the key-value store and, through the gossip protocol, its copy is sent to other components. You can read more about how the gossip protocol works at [How we're using gossip to improve Cortex and Loki availability](https://grafana.com/blog/2020/03/25/how-were-using-gossip-to-improve-cortex-and-loki-availability/). Once a component receives a key-value store from other components, it will merge the received key-value store with its own store and will send its own copy to other components eventually. After some iterations, the different components will all converge to the same key-value store. 

## Configuring memberlist in your Loki config

Configure `memberlist/join_members` with the list of addresses used to reach other components via service discovery. 

You can configure the `join_members` by using a CLI flag or by defining a section in the Loki configuration YAML. Both works accept the same formats and will have the same output. The CLI flag is `-memberlist.join=nodeA` and can be repeated to specify multiple join members and the YAML key is `memberlist/join_members`:
```yaml
memberlist:
  join_members:
    - nodeA
    - nodeB
```

The `join_members` you list are the nodes used to gossip the different rings used by Loki. A good thing to keep in mind when configuring the `join_members` configuration is that the fewer nodes are specified, the less gossiping is necessary, which reduces the number of messages sent. However, the more nodes specified, the more resilient your gossiping since all these nodes must go get down for your memberlist to stop working.

Both the CLI flag and YAML configuration expect given addresses to be in one of the following three formats: 
1. an IP address (ex: `192.168.1.1`)
2. a hostname (ex: `mycomputer.myorg.com`)
3. a DNS service discovery domain name (ex: `dnssrv+_memcached._tcp.memcached.namespace.svc.cluster.local`). For the full list of supported domain name formats, see [link](https://cortexmetrics.io/docs/configuration/arguments/#supported-discovery-modes). 

When a `memberlist_config` with least 1 `join_members` is defined, a kvstore of type memberlist is automatically configured for the distributor, ingester, and ruler rings unless otherwise specified in those components specific configuration sections.

This means you no longer have to specify

```
    kvstore:
      store: memberlist
```

for each of those components. 

## Check that your ring is being gossiped correctly
To check if your ring is being gossiped as expected, you can use the ring pages for each component and validate that the members of the ring that are listed match what you expect. See this_page for the list of the ring endpoints. 

## Try it out yourself
If you'd like to gain a little bit of familiarity with how memberlist works, try out this following little tutorial. 

We're going to setup Memberlist for a Loki cluster that is using the new
SSD mode (Simple, Scalable Deployment mode) with the help of the `docker-compose` tool. 

We’ll
1. start by spinning 6 Loki instances (3 in read mode, 3 in write mode) without setting Memberlist
2. we’ll check its status to see that no cooperation is being done
3. we’ll then properly configure memberlist
4. and finally, after this change, we’ll see that the ring is properly set for all Loki instances.


### Download Loki default configuration for Docker:
```bash
$ wget https://raw.githubusercontent.com/grafana/loki/main/cmd/loki/loki-docker-config.yaml
```

The yaml is also pasted below for reference:
```yaml
auth_enabled: false

server:
  http_listen_port: 3100

common:
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 24h

ruler:
  alertmanager_url: http://localhost:9093
```

### Modify the configuration to use `memberlist` instead of `inmemory` for its key-value store:

```diff
 ring:
   instance_addr: 127.0.0.1
   kvstore:
-      store: inmemory
+      store: memberlist
```

You'll note we've defined memberlist as the store, but we haven't defined any `join_members`, so the different Loki nodes won't know how to communicate to gossip the rings amongst themselves. 

### Define a `docker-compose` manifest to spin-up the 6 Loki nodes
3 of these nodes will be in
read mode and 3 will be in write mode.  Let's use ports
`:3100,:3101,:3102` for read nodes and ports `:3200,:3201,:3202` for write
Nodes. These ports will be used later to check what is happening on each node.

```yaml
version: "3.8" # docker-compose manifest version.
 
# Define a common Loki container configuration to be used by the other instances.
x-loki: &defaultloki
 image: grafana/loki:2.4.2
 # Mount manifest current folder to /opt/loki. This way, the containers are
 # able to access the Loki configuration file as long as it is in the current
 # folder of this manifest.
 volumes:
 - ./:/opt/loki/
 networks:
 - loki
 
services:
 # Define first Loki instance.
 loki-read1:
   <<: *defaultloki # Inherit from default Loki configuration.
   # Define the container port 3100 to be mirrored to your :3100 port. Later, we'll
   # access this port to make sure Memberlist is correctly set.
   ports:
   - 3100:3100
   # Set Loki args. The configuration file is the file that you modified at Step 2.
   # The target configure this instance to be on read mode.
   command: "-config.file=/opt/loki/loki-docker-config.yaml -target=read"
 
 loki-read2:
   <<: *defaultloki # Inherit from default Loki configuration.
   # Define the container port 3100 to be mirrored to your :3101 port.
   ports:
   - 3101:3100
   command: "-config.file=/opt/loki/loki-docker-config.yaml -target=read"
 
 loki-read3:
   <<: *defaultloki # Inherit from default Loki configuration.
   # Define the container port 3100 to be mirrored to your :3102 port.
   ports:
   - 3102:3100
   command: "-config.file=/opt/loki/loki-docker-config.yaml -target=read"
 
 loki-write1:
   <<: *defaultloki # Inherit from default Loki configuration.
   # Define the container port 3100 to be mirrored to your :3200 port.
   ports:
   - 3200:3100
   # Set Loki args. The configuration file is the file that you modified at Step 2.
   # The target configure this instance to be on write mode.
   command: "-config.file=/opt/loki/loki-docker-config.yaml -target=write"
 
 loki-write2:
   <<: *defaultloki # Inherit from default Loki configuration.
   # Define the container port 3100 to be mirrored to your :3201 port.
   ports:
   - 3201:3100
   command: "-config.file=/opt/loki/loki-docker-config.yaml -target=write"
 
 loki-write3:
   <<: *defaultloki # Inherit from default Loki configuration.
   # Define the container port 3100 to be mirrored to your :3202 port.
   ports:
   - 3202:3100
   command: "-config.file=/opt/loki/loki-docker-config.yaml -target=write"
 
networks:
 loki:
```

### Start up the containers
Run the following docker compose command in the same directory where you created your `docker-compose.yaml` file:

```bash
$ docker-compose up -d
```

### Check the ring status
We expect that the rings are not being gossipped to the whole cluster because we did not set the `join_members` field. Each of the 6 nodes should only know about itself, but not about any other nodes. 

Start by accessing in your browser the endpoint `/ring` which contains the state of the ingester ring. The complete URL will be `localhost:3100/ring` for the instance `loki-read1`. 

Since the ingester is a component of the write path, you should see that the `/ring` page only lists the ingester ring state when accessing the write nodes. So you'll see 1 member in the ingester ring when you access `localhost:3200/ring` (ingester ring page for `loki_write1`), `localhost:3201/ring` (ingester ring page for `loki_write2`), or `localhost:3202/ring` (ingester ring page for `loki_write3`). The only member in each of those ingester rings is the node itself. For example, `loki_write1` only knows about its own ingester, but not about the ingester on `loki_write3`. 

You won't see any members listed on the ingester ring page if you look at the read nodes (`localhost:3100/ring` for `loki_read1` or `localhost:3101/ring` for `loki_read2`, or `localhost:3101/ring` for `loki_read3`). This is because the read nodes do not have ingesters and they have no way of knowing about the ingesters on the write nodes. 

Now, try the same but for the endpoints `/scheduler/ring` and `/compactor/ring`, which contain the state of the query scheduler and compactor rings, respectively. You should see 1 element scheduler and compactor rings when checking each of the read nodes (check `localhost:3100/scheduler/ring`, `localhost:3101/scheduler/ring`, and `localhost:3102/scheduler/ring`). The 1 element represents the fact that each individual read node has a query-scheduler and a compactor, but it does not know about the query-scheduler and compactor on the other read nodes. 

You’ll get a 404 error when you check these same endpoints (`/scheduler/ring` and `/compactor/ring`) on the write nodes because the write nodes do not have a compactor or a scheduler. 


### Configure the different nodes to be discovered using Memberlist by setting
`join_members` 
Rather than doing this via the configuration file, we'll use the `-memberlist.join` CLI flag. 

We'll set all nodes to
gossip to node `loki-read1` for this example.  

In the docker-compose manifest, add the flag
`-memberlist.join=loki-read1` to all Loki nodes. Note that you don’t have to add this to node `loki-read1` (it doesn’t gossip to itself), but adding it doesn't hurt. 

```diff
 loki-read2:
-    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=read"
+    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=read -memberlist.join=loki-read1"
 ...
 ...
 loki-read3:
-    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=read"
+    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=read -memberlist.join=loki-read1"
 ...
 ...
 loki-write1:
-    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=write"
+    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=write -memberlist.join=loki-read1"
 ...
 ...
 loki-write2:
-    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=write"
+    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=write -memberlist.join=loki-read1"
 ...
 ...
 loki-write3:
-    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=write"
+    command: "-config.file=/opt/loki/loki-docker-config.yaml -target=write -memberlist.join=loki-read1"
```

We're setting `loki-read1` as the only gossip node for simplicity. However, this has the downside of having `loki-read1` as a single point of failure. In a production setup, you'd want several nodes to be able to gossip to each other for resiliency.

### Restart your cluster to use your new docker-compose manifest
```bash
$ docker-compose down
$ docker-compose up -d
```

### Check all the ring pages 
Access the different ring pages (ex: `/ring`, `/compactor/ring`, `/scheduler/ring`) for each of the 6 nodes (`localhost:{3100,3101,3102,3200,3201,3202}`). 

This time, you'll notice that all nodes (both read and write) have the correct picture of the ingester ring. It has 3 members (the 3 Loki `write` nodes). 

The `/compactor/ring` and `/scheduler/ring` endpoints will still only be accessible on the read nodes (`localhost:3100`, `localhost:3101`, and `localhost:3102`). However you'll notice all the read nodes now have the correct picture of the compactor ring and the scheduler ring. Each of these rings has 3 members (the 3 Loki `read` nodes). 

This is great! We're seeing that all the information about the ingester, compactor, and query-scheduler rings is being correctly gossiped through node `loki-read1`. All of the other nodes have a correct view of the system through `loki-read1`. 





