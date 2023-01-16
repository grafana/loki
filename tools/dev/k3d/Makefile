.PHONY: loki-distributed down add-repos update-repos prepare prepare-gel build-latest-image

IMAGE_TAG := $(shell ../../../tools/image-tag)
EXISTING_REGISTRY_PORT := $(shell k3d registry list -o json | jq -r '.[] | select(.name == "k3d-grafana") | .portMappings."5000/tcp" | .[0].HostPort')
REGISTRY_PORT ?= $(or $(EXISTING_REGISTRY_PORT),46453)

loki-distributed: prepare build-latest-image
	$(CURDIR)/scripts/create_cluster.sh loki-distributed $(REGISTRY_PORT)
	# wait 5s for the cluster to be ready
	sleep 5
	tk apply --ext-str registry="k3d-grafana:$(REGISTRY_PORT)" environments/loki-distributed

enterprise-logs: prepare-gel
	$(CURDIR)/scripts/create_cluster.sh enterprise-logs $(REGISTRY_PORT)
	# wait 5s for the cluster to be ready
	sleep 5
	$(MAKE) -C $(CURDIR) apply-enterprise-logs

helm-cluster: prepare
	ls -la $(CURDIR)
	ls -la $(CURDIR)/scripts
	$(CURDIR)/scripts/create_cluster.sh helm-cluster $(REGISTRY_PORT)
	# wait 5s for the cluster to be ready
	sleep 5
	$(MAKE) -C $(CURDIR) apply-helm-cluster

apply-enterprise-logs:
	tk apply --ext-str registry="k3d-grafana:$(REGISTRY_PORT)" environments/enterprise-logs

apply-helm-cluster:
	tk apply --ext-str registry="k3d-grafana:$(REGISTRY_PORT)" environments/helm-cluster

down:
	k3d cluster delete loki-distributed
	k3d cluster delete enterprise-logs
	k3d cluster delete helm-cluster

add-repos:
	helm repo add --force-update prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo add --force-update grafana https://grafana.github.io/helm-charts
	helm repo add --force-update minio https://charts.min.io/

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

# Secrets are kept in the Grafana Labs Shared 1password vault
# The op command used below is the 1password CLI tool.
# If you don't have access to the Grafana Labs Shared 1password vault
# you can provide any enterprise grafana license key in secrets/grafana.jwt
# and any enterprise logs license key in secrets/gel.jwt, though you may need
# to modify the deployment to provide the correct cluster name to match the
# license provided.
#
# When running the secrets target, make sure you first run `op signin`.
secrets: secrets/grafana.jwt secrets/gel.jwt

secrets/grafana.jwt:
	mkdir -p secrets/
	op document get "loki/grafana.jwt" --output=$(CURDIR)/secrets/grafana.jwt

secrets/gel.jwt:
	mkdir -p secrets/
	op document get "loki/gel.jwt" --output=$(CURDIR)/secrets/gel.jwt

prepare: create-registry update-repos
prepare-gel: prepare secrets

build-latest-image:
	make -C $(CURDIR)/../../.. loki-image
	docker tag grafana/loki:$(IMAGE_TAG) grafana.k3d.localhost:$(REGISTRY_PORT)/loki:latest
	docker push grafana.k3d.localhost:$(REGISTRY_PORT)/loki:latest

HELM_DIR := $(shell cd $(CURDIR)/../../../production/helm/loki && pwd)
helm-install-enterprise-logs:
	helm install enterprise-logs-test-fixture "$(HELM_DIR)" -n loki --create-namespace --values "$(CURDIR)/environments/helm-cluster/values/enterprise-logs.yaml"

helm-upgrade-enterprise-logs:
	helm upgrade enterprise-logs-test-fixture "$(HELM_DIR)" -n loki --values "$(CURDIR)/environments/helm-cluster/values/enterprise-logs.yaml"

helm-uninstall-enterprise-logs:
	helm uninstall enterprise-logs-test-fixture -n loki
