.PHONY: all build test clean build-image push-image
.DEFAULT_GOAL := all

IMAGE_PREFIX ?= grafana
IMAGE_TAG := $(shell ./tools/image-tag)

all: test build-image

build:
	go build -o loki-canary -v cmd/loki-canary/main.go

test:
	go test -v ./...

clean:
	rm -f ./loki-canary
	go clean ./...

build-image:
	docker build -t $(IMAGE_PREFIX)/loki-canary .
	docker tag $(IMAGE_PREFIX)/loki-canary $(IMAGE_PREFIX)/loki-canary:$(IMAGE_TAG)

push-image:
	docker push $(IMAGE_PREFIX)/loki-canary:$(IMAGE_TAG)
	docker push $(IMAGE_PREFIX)/loki-canary:latest