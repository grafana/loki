.PHONY: loki enterprise-logs helm-cluster down add-repos update-repos prepare prepare-gel build-latest-image
.PHONY: helm-cluster helm-install-enterprise-logs helm-upgrade-enterprise-logs helm-uninstall-enterprise-logs
.PHONY: helm-install-loki helm-upgrade-loki helm-uninstall-loki

IMAGE_TAG := $(shell ../../../tools/image-tag)
EXISTING_REGISTRY_PORT := $(shell k3d registry list -o json | jq -r '.[] | select(.name == "k3d-grafana") | .portMappings."5000/tcp" | .[0].HostPort')
REGISTRY_PORT ?= $(or $(EXISTING_REGISTRY_PORT),46453)

enterprise-logs: prepare-gel helm-cluster
	$(MAKE) -C $(CURDIR) apply-enterprise-helm-cluster
	echo "Waiting 5s for cluster to be ready for helm installation."
	echo "The helm install will take a while. It's useful to monitor progress using a tool like k9s."
	# wait 5s for tk apply to finish and cluster is ready for helm install
	sleep 5
	$(MAKE) -C $(CURDIR) helm-install-enterprise-logs
	echo "Helm installation finished. You can tear down this cluster with make down."

loki: prepare helm-cluster
	$(MAKE) -C $(CURDIR) apply-loki-helm-cluster
	echo "Waiting 5s for cluster to be ready for helm installation."
	# wait 5s for tk apply to finish and cluster is ready for helm install
	sleep 5
	$(MAKE) -C $(CURDIR) helm-install-loki
	echo "Helm installation finished. You can tear down this cluster with make down."

helm-cluster: prepare
	$(CURDIR)/scripts/create_cluster.sh helm-cluster $(REGISTRY_PORT)
	# wait 5s for the cluster to be ready
	sleep 5

apply-enterprise-helm-cluster:
	tk apply --ext-code enterprise=true environments/helm-cluster

apply-loki-helm-cluster:
	tk apply --ext-code enterprise=false environments/helm-cluster

down:
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
	op document get "loki/grafana.jwt" --output=$(CURDIR)/secrets/grafana.jwt || rm -f $(CURDIR)/secrets/grafana.jwt
	@if ! test -s "$(CURDIR)/secrets/grafana.jwt"; then \
		echo "Failed to fetch Grafana license from 1Password, are you logged in?"; \
		rm -f $(CURDIR)/secrets/grafana.jwt; \
		exit 1; \
	fi

secrets/gel.jwt:
	mkdir -p secrets/
	op document get "loki/gel.jwt" --output=$(CURDIR)/secrets/gel.jwt || rm -f $(CURDIR)/secrets/gel.jwt
	@if ! test -s "$(CURDIR)/secrets/gel.jwt"; then \
		echo "Failed to fetch GEL license from 1Password, are you logged in?"; \
		rm -f $(CURDIR)/secrets/gel.jwt; \
		exit 1; \
	fi

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

helm-install-loki:
	helm install loki "$(HELM_DIR)" -n loki --create-namespace --values "$(CURDIR)/environments/helm-cluster/values/loki.yaml"

helm-upgrade-loki:
	helm upgrade loki "$(HELM_DIR)" -n loki --values "$(CURDIR)/environments/helm-cluster/values/loki.yaml"

helm-uninstall-loki:
	helm uninstall loki -n loki
