
HOST_IP ?= host.docker.internal
TOPIC ?= promtail
RF ?= 1
PARTS ?= 3

BROKER_LIST := $(shell ../broker-list.sh $(HOST_IP))

start-kafka:
	docker-compose up -d

stop-kafka:
	docker-compose down

print-brokers:
	@echo $(BROKER_LIST)