.ONESHELL:
.DELETE_ON_ERROR:
export SHELL     := bash
export SHELLOPTS := pipefail:errexit
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rule

include docs.mk

PODMAN := $(shell if command -v podman >/dev/null 2>&1; then echo podman; else echo docker; fi)
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
