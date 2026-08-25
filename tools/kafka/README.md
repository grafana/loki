# Kafka tools

This folder contains tools for testing Loki's Kafka integration.

**Requirements:**

- docker and docker-compose
- make

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

## Consuming with Grafana Alloy

You can use [Grafana Alloy](https://grafana.com/docs/alloy/latest/) to consume from Kafka and send logs to Loki.

Make sure the brokers list (`make print-broker`) is correctly configured in your Alloy configuration file. Refer to the [Alloy documentation](https://grafana.com/docs/alloy/latest/reference/components/loki/loki.source.kafka/) for Kafka source configuration.
