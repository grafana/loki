.DEFAULT_GOAL := all
.PHONY: lint lint-yaml install-distributed install-single-binary uninstall update-chart update

# Optional image override, example: make install-distributed IMAGE=grafana/loki:2.9.0
IMAGE ?=

# Optional helm arguments, example: make install-distributed ARGS="--set loki.auth.enabled=true"
ARGS ?=

# Default arguments to disable affinity for testing
DEFAULT_ARGS = --set gateway.affinity=null \
	--set ingester.affinity=null \
	--set distributor.affinity=null \
	--set querier.affinity=null \
	--set queryFrontend.affinity=null \
	--set queryScheduler.affinity=null \
	--set indexGateway.affinity=null \
	--set compactor.affinity=null \
	--set ruler.affinity=null \
	--set backend.affinity=null \
	--set read.affinity=null \
	--set write.affinity=null \
	--set singleBinary.affinity=null \
	--set memcachedChunks.affinity=null \
	--set memcachedFrontend.affinity=null \
	--set memcachedIndexQueries.affinity=null \
	--set memcachedMetadata.affinity=null \
	--set memcachedResults.affinity=null \
	--set global.podAntiAffinity=null \
	--set global.podAntiAffinityTopologyKey=null

# Generate image override flag if IMAGE is provided
IMAGE_FLAG = $(if $(IMAGE),\
	$(eval PARTS=$(subst :, ,$(IMAGE)))\
	$(eval REPO_PARTS=$(subst /, ,$(word 1,$(PARTS))))\
	$(eval TAG=$(word 2,$(PARTS)))\
	$(eval REPO_COUNT=$(words $(REPO_PARTS)))\
	$(if $(filter 3,$(REPO_COUNT)),\
		--set loki.image.registry=$(word 1,$(REPO_PARTS))/$(word 2,$(REPO_PARTS)) --set loki.image.repository=$(word 3,$(REPO_PARTS)),\
		--set loki.image.registry=$(word 1,$(REPO_PARTS)) --set loki.image.repository=$(word 2,$(REPO_PARTS))\
	) --set loki.image.tag=$(TAG),)

lint: lint-yaml

lint-yaml:
	yamllint -c $(CURDIR)/src/.yamllint.yaml $(CURDIR)/src

# Helm chart installation targets
install-distributed:
	helm upgrade --install loki . \
		-f distributed-values.yaml \
		--create-namespace \
		--namespace loki \
		$(DEFAULT_ARGS) \
		$(IMAGE_FLAG) \
		$(ARGS)

install-single-binary:
	helm upgrade --install loki . \
		-f single-binary-values.yaml \
		--create-namespace \
		--namespace loki \
		$(DEFAULT_ARGS) \
		$(IMAGE_FLAG) \
		$(ARGS)

# Uninstall Loki helm release and optionally delete the namespace
uninstall:
	helm uninstall loki --namespace loki
	kubectl delete namespace loki --ignore-not-found

# Update Helm chart dependencies
update-chart:
	helm dependency update .

# Update existing installation with latest changes
update:
	@if [ "$$(helm get values loki -n loki -o yaml | grep "deploymentMode: Distributed")" ]; then \
		echo "Updating distributed deployment..."; \
		helm upgrade loki . -f distributed-values.yaml --namespace loki $(DEFAULT_ARGS) $(IMAGE_FLAG) $(ARGS); \
	else \
		echo "Updating single binary deployment..."; \
		helm upgrade loki . -f single-binary-values.yaml --namespace loki $(DEFAULT_ARGS) $(IMAGE_FLAG) $(ARGS); \
	fi
