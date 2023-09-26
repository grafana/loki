
HOST_IP ?= host.docker.internal
TOPIC ?= promtail
RF ?= 1
PARTS ?= 3

BROKER_LIST := $(shell ../broker-list.sh $(HOST_IP))

start-kafka:
	docker-compose up -d zookeeper
	docker-compose exec zookeeper kafka-configs \
		--zookeeper localhost:22181 \
		--alter \
		--add-config 'SCRAM-SHA-512=[iterations=8192,password=kafkaadmin-pass],SCRAM-SHA-512=[password=kafkaadmin-pass]' \
		--entity-type users --entity-name kafkaadmin
	docker-compose up -d kafka

stop-kafka:
	docker-compose down

print-brokers:
	@echo $(BROKER_LIST)