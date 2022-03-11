.PHONY: loki-distributed down add-repos update-repos prepare build-latest-image

IMAGE_TAG := $(shell ../../../tools/image-tag)
REGISTRY_PORT ?= 45629
CLUSTER_NAME := "loki-distributed"

loki-distributed: prepare
	$(CURDIR)/scripts/create_cluster.sh $(CLUSTER_NAME)

down:
	k3d cluster delete loki-distributed

add-repos:
	helm repo add --force-update prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo add --force-update grafana https://grafana.github.io/helm-charts
	helm repo add --force-update minio https://helm.min.io

update-repos: add-repos
	helm repo update
	tk tool charts vendor
	jb update

create-registry:
	@if ! k3d registry list | grep -q -m 1 grafana; then \
		echo "Creating registry"; \
		k3d registry create grafana --port $(REGISTRY_PORT); \
	else \
		echo "Registry already exists"; \
	fi

create-cluster:
	@if ! k3d cluster list | grep -q -m 1 $(CLUSTER_NAME); then \
		echo "Creating cluster"; \
		k3d cluster create $(CLUSTER_NAME) \
			--servers 1 \
			--agents 3 \
			--registry-use "k3d-grafana:$(REGISTRY_PORT)" \
			--wait; \
	else \
		echo "Cluster already exists"; \
	fi

prepare: create-registry create-cluster update-repos build-latest-image

build-latest-image:
	make -C $(CURDIR)/../../.. loki-image
	docker tag grafana/loki:$(IMAGE_TAG) k3d-grafana:$(REGISTRY_PORT)/loki:latest
	docker push k3d-grafana:$(REGISTRY_PORT)/loki:latest
