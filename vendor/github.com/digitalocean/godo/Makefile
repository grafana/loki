# Makefile for godo automated release process

# Variables
GO_VERSION := $(shell go version 2>/dev/null | awk '{print $$3}' | sed 's/go//')
GODO_VERSION := $(shell grep -E '^\s*libraryVersion\s*=' godo.go | sed 's/.*"\(.*\)".*/\1/')
ROOT_DIR := $(dir $(realpath $(lastword $(MAKEFILE_LIST))))
ORIGIN ?= origin
COMMIT ?= HEAD
BUMP ?= patch

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'; \
	printf "\nNOTE: Use 'make BUMP=(bugfix|feature|breaking) bump_version' to create a release.\n"

.PHONY: dev-dependencies
dev-dependencies: ## Install development tooling
	@go install github.com/digitalocean/github-changelog-generator@latest
	@if ! command -v jq &> /dev/null; then \
		echo "WARNING: jq not found. Please install jq manually."; \
	fi
	@if ! command -v gh &> /dev/null; then \
		echo "WARNING: GitHub CLI not found. Please install gh manually."; \
	fi

.PHONY: install
install: ## Install dependencies
ifneq (, $(shell which go))
	@go mod download
	@go mod tidy
else
	@(echo "go is not installed. See https://golang.org/doc/install for more info."; exit 1)
endif

.PHONY: test
test: install ## Run tests
	@go test -mod=vendor .

.PHONY: test-verbose
test-verbose: install ## Run tests with verbose output
	@go test -mod=vendor -v .

.PHONY: lint
lint: install ## Run linting
	@go fmt ./...
	@go vet ./...

.PHONY: _install_github_release_notes
_install_github_release_notes:
	@go install github.com/digitalocean/github-changelog-generator@latest

.PHONY: changes
changes: _install_github_release_notes ## Review merged PRs since last release
	@echo "==> Merged PRs since last release"
	@echo ""
	@github-changelog-generator -org digitalocean -repo godo

.PHONY: version
version: ## Show current version
	@echo "godo version: $(GODO_VERSION)"
	@echo "go version: $(GO_VERSION)"

.PHONY: bump_version
bump_version: ## Bumps the version
	@echo "==> BUMP=$(BUMP) bump_version"
	@echo ""
	@ORIGIN=$(ORIGIN) scripts/bumpversion.sh

.PHONY: tag
tag: ## Tags a release and prints changelog info
	@echo "==> ORIGIN=$(ORIGIN) COMMIT=$(COMMIT) tag"
	@echo ""
	@ORIGIN=$(ORIGIN) scripts/tag.sh; \
	NEW_TAG=$$(git describe --tags --abbrev=0); \
	echo "==> Generating changelog for tag $$NEW_TAG"; \