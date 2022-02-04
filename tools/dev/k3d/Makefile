.PHONY: loki-distributed down add-repos update-repos prepare build-latest-image

IMAGE_TAG := $(shell ../../../tools/image-tag)
REGISTRY_PORT ?= 45629

loki-distributed: prepare
	$(CURDIR)/scripts/create_cluster.sh "loki-distributed"

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

prepare: update-repos build-latest-image

build-latest-image:
	make -C $(CURDIR)/../../.. loki-image
	docker tag grafana/loki:$(IMAGE_TAG) k3d-grafana:$(REGISTRY_PORT)/loki:latest
	docker push k3d-grafana:$(REGISTRY_PORT)/loki:latest
