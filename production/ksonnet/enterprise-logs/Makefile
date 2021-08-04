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

jsonnetfile.lock.json: ## Update the locked dependency versions.
jsonnetfile.lock.json: jsonnetfile.json
	jb update

vendor: ## Vendor dependencies.
vendor: jsonnetfile.json
	jb install

lib:
	mkdir -p $@

lib/k.libsonnet: ## Install implicit k.libsonnet dependency.
lib/k.libsonnet: jsonnetfile.json | lib
	lib="github.com/jsonnet-libs/k8s-libsonnet"
	version=$$(jq --arg repo https://$${lib}.git -r -M \
								'.dependencies[] | select(.source.git.remote == $$repo) | .source.git.subdir' \
								jsonnetfile.json)
	printf "(import '%s/%s/main.libsonnet')" $${lib} $${version} > $@

.eval: ## Evaluate the library Jsonnet.
.eval: eval.jsonnet main.libsonnet lib/k.libsonnet vendor
	tmp=$$(mktemp); if tk eval $< | tee $${tmp}; then mv $${tmp} $@; fi
