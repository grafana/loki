# Kafka tools

This folder contains tools for testing promtail <-> kafka integration.

**Requirements:**

- docker and docker-compose
- make
- go > 1.15 for consuming using code source of promtail

## Running kafka locally

To start kafka use `make start-kafka` this should start `kafka` and a `zookeeper` docker compose stack.
To discover available brokers you can use the `make print-brokers`.

Finally to stop the compose stack use `make stop-kafka`. This will result in all topics being lost with their messages.

## Running secure kafka locally

To test authentication, you need to start the Kafka container which is configured with authentication.

You can also use `make start-kafka` in appropriate directory like `sasl-scram` you need.

In addition, you need to create certificates using `make create-certs` when using SSL/TLS.

If you don't need to authenticate, you should use the tools in `plain` directory.

## Working with Topic

In Kafka before sending messages you need to create and select the topic you want to use for the exchange.

To create a new topic use: `make create-topic`, by default the topic name will be `promtail` if you wish to overrides it you can use the `TOPIC` variable as shown below:

```bash
TOPIC=new-topic make create-topic
```

To list all available topics use the `make list-topics` target.

```bash
 make list-topics
__consumer_offsets
promtail
```

To describe a topic use the `make describe-topic` target.

```bash
 TOPIC=new-topic make describe-topic

Topic: new-topic        PartitionCount: 3       ReplicationFactor: 1    Configs: segment.bytes=1073741824
        Topic: new-topic        Partition: 0    Leader: 1001    Replicas: 1001  Isr: 1001
        Topic: new-topic        Partition: 1    Leader: 1001    Replicas: 1001  Isr: 1001
        Topic: new-topic        Partition: 2    Leader: 1001    Replicas: 1001  Isr: 1001
```

As you can see by default each topic is assigned by default  3 partitions and a replication factor of 1. You can change those default using respectively `PARTS` and `RF` variable, for example the example below will create a topic with 4 partitions:

```bash
PARTS=4 TOPIC=new-topic-part make create-topic
```

Partitions in kafka are used for scaling reads and writes. Each partitions is replicated on multiple machines.

> WARNING: Order of messages is guaranteed on a single partition but not across a single topic.

## Producing messages

You can start sending message using `make producer` target, it will start reading newlines and send them to the default topic (`promtail`):

```bash
TOPIC=new-topic make producer

Producing messages to topic new-topic...
Write a message and press Enter
>hello world !
>
```

## Consuming with promtail

You can run promtail in `dry-run` mode and connect it to the local kafka to try out the integration.

Before doing so make sure the brokers list (`make print-broker`) is correctly configured in your promtail configuration file, see for example the [kafka example](../../clients/cmd/promtail/promtail-kafka.yaml).

```bash
go run ../../clients/cmd/promtail/main.go --dry-run --config.file ../../clients/cmd/promtail/promtail-kafka.yaml

Clients configured:
----------------------
url: http://localhost:3100/loki/api/v1/push
batchwait: 1s
batchsize: 1048576
follow_redirects: false
backoff_config:
  min_period: 500ms
  max_period: 5m0s
  max_retries: 10
timeout: 10s
tenant_id: ""

level=info ts=2021-11-02T10:44:14.137894Z caller=server.go:260 http=[::]:9080 grpc=[::]:59237 msg="server listening on addresses"
level=info ts=2021-11-02T10:44:14.138059Z caller=main.go:119 msg="Starting Promtail" version="(version=, branch=, revision=)"
level=info ts=2021-11-02T10:44:14.139308Z caller=target_syncer.go:133 msg="new topics received" topics=[promtail]
level=info ts=2021-11-02T10:44:14.139337Z caller=consumer.go:50 msg="starting consumer" topics=[promtail]
level=info ts=2021-11-02T10:44:14.153164Z caller=consumer.go:92 msg="consuming topic" details="member_id=sarama-8cfa484d-2a04-458a-a0c0-4506c7a0969f generation_id=5 topic=promtail partition=1 initial_offset=12"
level=info ts=2021-11-02T10:44:14.153673Z caller=consumer.go:92 msg="consuming topic" details="member_id=sarama-8cfa484d-2a04-458a-a0c0-4506c7a0969f generation_id=5 topic=promtail partition=0 initial_offset=13"
level=info ts=2021-11-02T10:44:14.153927Z caller=consumer.go:92 msg="consuming topic" details="member_id=sarama-8cfa484d-2a04-458a-a0c0-4506c7a0969f generation_id=5 topic=promtail partition=2 initial_offset=10"

2021-11-02T11:47:08.849115+0100{group="some_group", job="kafka", partition="1", topic="promtail"}       hello world !
```

> Alternatively you can use the binary or docker version of promtail.
