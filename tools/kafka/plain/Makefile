
HOST_IP ?= host.docker.internal
TOPIC ?= promtail
RF ?= 1
PARTS ?= 3

BROKER_LIST := $(shell ../broker-list.sh $(HOST_IP))
DOCKER_RUN := docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -e HOST_IP=$(HOST_IP) -i -t wurstmeister/kafka /bin/bash -c

start-kafka:
	docker-compose up -d

stop-kafka:
	docker-compose down

print-brokers:
	@echo $(BROKER_LIST)

# TOPIC=bar make create-topic
create-topic:
	@$(DOCKER_RUN) "kafka-topics.sh --create -bootstrap-server $(BROKER_LIST) --replication-factor $(RF) --partitions $(PARTS) --topic $(TOPIC)"

# TOPIC=bar make describe-topic
describe-topic:
	@$(DOCKER_RUN) "kafka-topics.sh --describe -bootstrap-server $(BROKER_LIST) --topic $(TOPIC)"

list-topics:
	@$(DOCKER_RUN) "kafka-topics.sh --list -bootstrap-server $(BROKER_LIST)"

# TOPIC=bar make producer
producer:
	@echo "Producing messages to topic $(TOPIC)... "
	@echo "Write a message and press Enter"
	@$(DOCKER_RUN) "kafka-console-producer.sh --broker-list $(BROKER_LIST) --topic $(TOPIC)"
