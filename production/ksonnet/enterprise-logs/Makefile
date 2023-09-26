include variables.mk

.ONESHELL:
.DELETE_ON_ERROR:
export SHELL     := bash
export SHELLOPTS := pipefail:errexit
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rule

# Adapted from https://suva.sh/posts/well-documented-makefiles/
.PHONY: help
help: ## Display this help.
help:
	@awk 'BEGIN {FS = ": ##"; printf "Usage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_\.\-\/% ]+: ##/ { printf "  %-45s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

jsonnetfile.lock.json: ## Update the locked dependency versions for the library.
jsonnetfile.lock.json: jsonnetfile.json
	jb update

test: ## Evaluate the library Jsonnet.
test: test/.eval
	:

test/.eval: # Cache testing results.
test/.eval: test/eval.jsonnet main.libsonnet test/lib/k.libsonnet test/vendor
	cd $(@D); tmp=$$(mktemp); if tk eval $(<F) | tee $${tmp}; then mv $${tmp} $(@F); fi

test/lib:
	mkdir -p $@

test/lib/k.libsonnet: # Install implicit k.libsonnet dependency used in testing.
test/lib/k.libsonnet: test/lib
	printf "(import 'github.com/jsonnet-libs/k8s-libsonnet/%s/main.libsonnet')" $(K8S_VERSION) > $@

test/jsonnetfile.json: ## Update the jsonnetfile used in testing.
test/jsonnetfile.json: test/jsonnetfile.jsonnet variables.mk jsonnetfile.json
	jsonnet --tla-str k8sVersion=$(K8S_VERSION) $< > $@

test/jsonnetfile.lock.json test/vendor: ## Update the locked dependency versions used in testing.
test/jsonnetfile.lock.json test/vendor: test/jsonnetfile.json
	cd $(@D); jb update

.PHONY: k3d-cluster-create
k3d-cluster-create: ## Create a development k3d cluster.
	./scripts/k3d-cluster create

.PHONY: k3d-cluster-create
k3d-cluster-delete: ## Delete a development k3d cluster.
k3d-cluster-delete:
	./scripts/k3d-cluster delete
