# Copyright 2025 The go-yaml Project Contributors
# SPDX-License-Identifier: Apache-2.0

# Auto-install https://github.com/makeplus/makes at specific commit:
MAKES := .cache/makes
MAKES-LOCAL := .cache/local
MAKES-COMMIT ?= 4e48a743c3652b88adc4a257398d895a801e6d11
$(shell [ -d $(MAKES) ] || ( \
  git clone -q https://github.com/makeplus/makes $(MAKES) && \
  git -C $(MAKES) reset -q --hard $(MAKES-COMMIT)))
ifneq ($(shell git -C $(MAKES) rev-parse HEAD), \
       $(shell git -C $(MAKES) rev-parse $(MAKES-COMMIT)))
$(error $(MAKES) is not at the correct commit: $(MAKES-COMMIT). \
	Remove $(MAKES) and try again.)
endif

include $(MAKES)/init.mk
include $(MAKES)/shellcheck.mk

# Auto-install go unless GO_YAML_PATH is set:
ifdef GO_YAML_PATH
override export PATH := $(GO_YAML_PATH):$(PATH)
else
GO-VERSION ?= 1.25.5
endif
GO-VERSION-NEEDED := $(GO-VERSION)

# yaml-test-suite info:
YTS-URL ?= https://github.com/yaml/yaml-test-suite
YTS-TAG ?= data-2022-01-17
YTS-DIR := yts/testdata/$(YTS-TAG)

CLI-BINARY := go-yaml

# Pager for viewing documentation:
PAGER ?= less -FRX

# Setup and include go.mk and shell.mk:

# We need to limit `find` to avoid dirs like `.cache/` and any git worktrees,
# as this makes `make` operations very slow:
REPO-DIRS := $(shell find * -maxdepth 0 -type d \
	       ! -exec test -f {}/.git \; -print)
GO-FILES := $(shell find $(REPO-DIRS) -name '*.go')

ifndef GO-VERSION-NEEDED
GO-NO-DEP-GO := true
endif

include $(MAKES)/go.mk

# Set this from the `make` command to override:
GOLANGCI-LINT-VERSION ?= v2.8.0
GOLANGCI-LINT-INSTALLER := \
  https://github.com/golangci/golangci-lint/raw/main/install.sh
GOLANGCI-LINT := $(LOCAL-BIN)/golangci-lint
GOLANGCI-LINT-VERSIONED := $(GOLANGCI-LINT)-$(GOLANGCI-LINT-VERSION)

SHELL-DEPS += $(GOLANGCI-LINT-VERSIONED)

ifdef GO-VERSION-NEEDED
GO-DEPS += $(GO)
else
SHELL-DEPS := $(filter-out $(GO),$(SHELL-DEPS))
endif

SHELL-NAME := makes go-yaml
include $(MAKES)/clean.mk
include $(MAKES)/shell.mk

MAKES-CLEAN := $(CLI-BINARY) $(GOLANGCI-LINT)
MAKES-REALCLEAN := $(dir $(YTS-DIR))

SHELL-SCRIPTS = \
  util/common.bash \
  $(shell grep -rl '^.!/usr/bin/env bash' util | \
          grep -v '\.sw')

COVER-TESTS := \
 . \
 ./cmd/... \
 ./internal/... \

# v=1 for verbose
MAKE := $(MAKE) --no-print-directory

v ?=
cover ?=
fuzz ?=
time ?= 60s
opts ?=

TEST-OPTS := \
$(if $v, -v)\
$(if $(cover), --cover)\
$(if $(fuzz), --fuzz=FuzzEncodeFromJSON --fuzztime=$(time))\
$(if $(opts), $(opts))\

# Test rules:
test: test-main test-internal test-cmd test-yts-all test-shell
	@echo 'ALL TESTS PASS'

check:
	$(MAKE) fmt
	$(MAKE) tidy
	$(MAKE) lint
	$(MAKE) test

test-main: $(GO-DEPS)
	go test .$(TEST-OPTS)
	@echo 'ALL MAIN FILES PASS'

test-cmd: $(GO-DEPS)
	go test ./cmd/...$(TEST-OPTS)
	@echo 'ALL CMD FILES PASS'

test-internal: $(GO-DEPS)
	go test ./internal/...$(TEST-OPTS)
	@echo 'ALL INTERNAL FILES PASS'

test-cover: $(GO-DEPS)
	go test . $(COVER-TESTS) -vet=off --cover$(TEST-OPTS)

test-yts: $(GO-DEPS) $(YTS-DIR)
	go test ./yts$(TEST-OPTS)

test-yts-all: $(GO-DEPS) $(YTS-DIR)
	@echo 'Testing yaml-test-suite'
	util/yaml-test-suite all

test-yts-fail: $(GO-DEPS) $(YTS-DIR)
	@echo 'Testing yaml-test-suite failures'
	util/yaml-test-suite fail

test-shell: $(SHELLCHECK)
	shellcheck $(SHELL-SCRIPTS)
	@echo 'ALL SHELL FILES PASS'

test-count: $(GO-DEPS)
	util/test-count

yts-dir: $(YTS-DIR)

get-test-data: $(YTS-DIR)

# Install golangci-lint for GitHub Actions:
golangci-lint-install: $(GOLANGCI-LINT)

fmt: $(GOLANGCI-LINT-VERSIONED)
	$< fmt ./...

lint: $(GOLANGCI-LINT-VERSIONED)
	$< run ./...

tidy: $(GO-DEPS)
	go mod tidy

cli: $(CLI-BINARY)

$(CLI-BINARY): $(GO)
	go build -o $@ ./cmd/$@

run-examples: $(GO)
	@for dir in example/*/; do \
	  (set -x; go run "$${dir}main.go") || \
	  { echo "$$dir failed"; break; }; \
	done

# CLI documentation (go doc) - view in terminal:
doc: $(GO-DEPS)
	@go doc -all . | $(PAGER)

# HTTP documentation server - opens browser:
doc-http: $(GO-DEPS)
	go doc -http -all

# Setup rules:
$(YTS-DIR):
	git clone -q $(YTS-URL) $@
	git -C $@ checkout -q $(YTS-TAG)

# Downloads golangci-lint binary and moves to versioned path
# (.cache/local/bin/golangci-lint-<version>).
$(GOLANGCI-LINT-VERSIONED): $(GO-DEPS)
	curl -sSfL $(GOLANGCI-LINT-INSTALLER) | \
	  bash -s -- -b $(LOCAL-BIN) $(GOLANGCI-LINT-VERSION)
	mv $(GOLANGCI-LINT) $@

# Moves golangci-lint-<version> to golangci-lint for CI requirement
$(GOLANGCI-LINT): $(GOLANGCI-LINT-VERSIONED)
	cp $< $@
