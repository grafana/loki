.ONESHELL:
.DELETE_ON_ERROR:
export SHELL     := bash
export SHELLOPTS := pipefail:errexit
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rule

GIT_ROOT := $(shell git rev-parse --show-toplevel)
# Support podman over Docker if it is available.
PODMAN  := $(shell if command -v podman >/dev/null 2>&1; then echo podman; else echo docker; fi)

# This allows ports and base URL to be overridden, so services like ngrok.io can
# be used to share a local running docs instances.
DOCS_HOST_PORT   := 3002
DOCS_LISTEN_PORT := 3002
DOCS_BASE_URL    := "localhost:$(DOCS_HOST_PORT)"

PROJECT         := loki
PROJECT_VERSION := latest
ifeq ($(PROJECT_VERSION),)
PROJECT_URL         := http://$(DOCS_BASE_URL)/docs/$(PROJECT)/
PROJECT_CONTENT_DIR := /hugo/content/docs/$(PROJECT)
MAKE_SERVER         := make server
else
PROJECT_URL               := http://$(DOCS_BASE_URL)/docs/$(PROJECT)/$(PROJECT_VERSION)
PROJECT_CONTENT_DIR       := /hugo/content/docs/$(PROJECT)/$(PROJECT_VERSION)
PROJECT_REDIRECT_TEMPLATE := "'---\\nredirectURL: /docs/$(PROJECT)/$(PROJECT_VERSION)\\ntype: redirect\\n---\\n'"
MAKE_SERVER               := /usr/bin/env bash -c "echo -e $(PROJECT_REDIRECT_TEMPLATE) > /hugo/content/docs/$(PROJECT)/_index.md && make server"
endif

DOCS_IMAGE     := grafana/docs-base:latest
DOCS_CONTAINER := $(PROJECT)-docs

HUGO_REFLINKSERRORLEVEL ?= WARNING

.PHONY: docs-rm
docs-rm: ## Remove the docs container.
	$(PODMAN) rm -f $(DOCS_CONTAINER)

.PHONY: docs-pull
docs-pull: ## Pull documentation base image.
	$(PODMAN) pull $(DOCS_IMAGE)

.PHONY: docs
docs: ## Serve documentation locally.
docs: docs-pull
	@echo "Documentation will be served at:"
	echo $(PROJECT_URL)
	echo ""
	if [[ -z $${NON_INTERACTIVE} ]]; then \
		read -p "Press enter to continue"; \
	fi
	$(PODMAN) run \
		--init \
		--interactive \
		--env HUGO_REFLINKSERRORLEVEL=$(HUGO_REFLINKSERRORLEVEL) \
		--name $(DOCS_CONTAINER) \
		--platform linux/amd64 \
		--publish $(DOCS_HOST_PORT):$(DOCS_LISTEN_PORT) \
		--rm \
		--volume $(GIT_ROOT)/docs/sources:$(PROJECT_CONTENT_DIR):ro,z \
		--tty \
		$(DOCS_IMAGE) \
		$(MAKE_SERVER)
