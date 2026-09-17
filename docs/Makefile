.ONESHELL:
.DELETE_ON_ERROR:
export SHELL     := bash
export SHELLOPTS := pipefail:errexit
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rule

include docs.mk

PODMAN := $(shell if command -v podman >/dev/null 2>&1; then echo podman; else echo docker; fi)
BUILD_IN_CONTAINER ?= true
