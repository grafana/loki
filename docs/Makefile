.ONESHELL:
.DELETE_ON_ERROR:
export SHELL     := bash
export SHELLOPTS := pipefail:errexit
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rule

include docs.mk

PODMAN := $(shell if command -v podman >/dev/null 2>&1; then echo podman; else echo docker; fi)
BUILD_IN_CONTAINER ?= true

.PHONY: sources/setup/install/helm/reference.md
sources/setup/install/helm/reference.md: ../production/helm/loki/reference.md.gotmpl ../production/helm/loki/values.yaml
ifeq ($(BUILD_IN_CONTAINER),true)
	$(PODMAN) run --rm --volume "$(realpath ..):/helm-docs" -u "$$(id -u)" "docker.io/jnorwood/helm-docs:v1.11.0" \
		-c /helm-docs/production/helm/ \
		-t reference.md.gotmpl \
		-o reference.md
	$(PODMAN) run --rm --volume "$(realpath ..):/helm-docs" -u "$$(id -u)" "docker.io/jnorwood/helm-docs:v1.11.0" \
		-c /helm-docs/production/helm/ \
		-t README.md.gotmpl \
		-o README.md
else
	helm-docs -c ../production/helm/ -t reference.md.gotmpl -o reference.md
	helm-docs -c ../production/helm/ -t README.md.gotmpl -o README.md
endif
	mv "$(basename $<)" "$@"
