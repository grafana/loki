.DEFAULT_GOAL: help

# Adapted from https://www.thapaliya.com/en/writings/well-documented-makefiles/
.PHONY: help
help: ## Display this help.
help:
	@awk 'BEGIN {FS = ": ##"; printf "Usage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_\.\-\/%]+: ##/ { printf "  %-45s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

BUILD_IN_CONTAINER ?= true

sources/installation/helm/reference.md: ../production/helm/loki/reference.md.gotmpl
ifeq ($(BUILD_IN_CONTAINER),true)
	$(PODMAN) run --rm --volume "$(realpath ..):/helm-docs:z" -u "$$(id -u)" "docker.io/jnorwood/helm-docs:v1.11.0" \
		-c /helm-docs/production/helm/ \
		-t reference.md.gotmpl \
		-o reference.md
else
	helm-docs -c ../production/helm/ -t reference.md.gotmpl -o reference.md
endif
	mv "$(basename $<)" "$@"

docs: sources/installation/helm/reference.md

include docs.mk
