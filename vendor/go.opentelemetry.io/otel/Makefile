# Copyright The OpenTelemetry Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

<<<<<<< HEAD
EXAMPLES := $(shell ./get_main_pkgs.sh ./example)
TOOLS_MOD_DIR := ./internal/tools

# All source code and documents. Used in spell check.
ALL_DOCS := $(shell find . -name '*.md' -type f | sort)
# All directories with go.mod files related to opentelemetry library. Used for building, testing and linting.
ALL_GO_MOD_DIRS := $(filter-out $(TOOLS_MOD_DIR), $(shell find . -type f -name 'go.mod' -exec dirname {} \; | egrep -v '^./example' | sort)) $(shell find ./example -type f -name 'go.mod' -exec dirname {} \; | sort)
=======
TOOLS_MOD_DIR := ./internal/tools

ALL_DOCS := $(shell find . -name '*.md' -type f | sort)
ALL_GO_MOD_DIRS := $(shell find . -type f -name 'go.mod' -exec dirname {} \; | sort)
OTEL_GO_MOD_DIRS := $(filter-out $(TOOLS_MOD_DIR), $(ALL_GO_MOD_DIRS))
>>>>>>> main
ALL_COVERAGE_MOD_DIRS := $(shell find . -type f -name 'go.mod' -exec dirname {} \; | egrep -v '^./example|^$(TOOLS_MOD_DIR)' | sort)

GO = go
TIMEOUT = 60

.DEFAULT_GOAL := precommit

.PHONY: precommit ci
<<<<<<< HEAD
precommit: dependabot-check license-check lint build examples test-default
ci: precommit check-clean-work-tree test-coverage
=======
precommit: license-check misspell go-mod-tidy golangci-lint-fix test-default
ci: dependabot-check license-check lint vanity-import-check build test-default check-clean-work-tree test-coverage
>>>>>>> main

# Tools

TOOLS = $(CURDIR)/.tools

$(TOOLS):
	@mkdir -p $@
$(TOOLS)/%: | $(TOOLS)
	cd $(TOOLS_MOD_DIR) && \
	$(GO) build -o $@ $(PACKAGE)

MULTIMOD = $(TOOLS)/multimod
$(TOOLS)/multimod: PACKAGE=go.opentelemetry.io/build-tools/multimod

SEMCONVGEN = $(TOOLS)/semconvgen
$(TOOLS)/semconvgen: PACKAGE=go.opentelemetry.io/build-tools/semconvgen

CROSSLINK = $(TOOLS)/crosslink
$(TOOLS)/crosslink: PACKAGE=go.opentelemetry.io/otel/$(TOOLS_MOD_DIR)/crosslink

GOLANGCI_LINT = $(TOOLS)/golangci-lint
$(TOOLS)/golangci-lint: PACKAGE=github.com/golangci/golangci-lint/cmd/golangci-lint

MISSPELL = $(TOOLS)/misspell
$(TOOLS)/misspell: PACKAGE=github.com/client9/misspell/cmd/misspell

GOCOVMERGE = $(TOOLS)/gocovmerge
$(TOOLS)/gocovmerge: PACKAGE=github.com/wadey/gocovmerge

STRINGER = $(TOOLS)/stringer
$(TOOLS)/stringer: PACKAGE=golang.org/x/tools/cmd/stringer

PORTO = $(TOOLS)/porto
$(TOOLS)/porto: PACKAGE=github.com/jcchavezs/porto/cmd/porto

GOJQ = $(TOOLS)/gojq
$(TOOLS)/gojq: PACKAGE=github.com/itchyny/gojq/cmd/gojq

.PHONY: tools
tools: $(CROSSLINK) $(GOLANGCI_LINT) $(MISSPELL) $(GOCOVMERGE) $(STRINGER) $(PORTO) $(GOJQ) $(SEMCONVGEN) $(MULTIMOD)

# Build

<<<<<<< HEAD
.PHONY: examples generate build
examples:
	@set -e; for dir in $(EXAMPLES); do \
	  echo "$(GO) build $${dir}/..."; \
	  (cd "$${dir}" && \
	   $(GO) build .); \
	done

generate: $(STRINGER) $(PORTO)
	set -e; for dir in $(ALL_GO_MOD_DIRS); do \
	  echo "$(GO) generate $${dir}/..."; \
	  (cd "$${dir}" && \
	    PATH="$(TOOLS):$${PATH}" $(GO) generate ./... && \
		$(PORTO) -w .); \
	done

build: generate
	# Build all package code including testing code.
	set -e; for dir in $(ALL_GO_MOD_DIRS); do \
	  echo "$(GO) build $${dir}/..."; \
	  (cd "$${dir}" && \
	    $(GO) build ./... && \
		$(GO) list ./... \
		  | grep -v third_party \
		  | xargs $(GO) test -vet=off -run xxxxxMatchNothingxxxxx >/dev/null); \
	done
=======
.PHONY: generate build

generate: $(OTEL_GO_MOD_DIRS:%=generate/%)
generate/%: DIR=$*
generate/%: | $(STRINGER) $(PORTO)
	@echo "$(GO) generate $(DIR)/..." \
		&& cd $(DIR) \
		&& PATH="$(TOOLS):$${PATH}" $(GO) generate ./... && $(PORTO) -w .

build: generate $(OTEL_GO_MOD_DIRS:%=build/%) $(OTEL_GO_MOD_DIRS:%=build-tests/%)
build/%: DIR=$*
build/%:
	@echo "$(GO) build $(DIR)/..." \
		&& cd $(DIR) \
		&& $(GO) build ./...

build-tests/%: DIR=$*
build-tests/%:
	@echo "$(GO) build tests $(DIR)/..." \
		&& cd $(DIR) \
		&& $(GO) list ./... \
		| grep -v third_party \
		| xargs $(GO) test -vet=off -run xxxxxMatchNothingxxxxx >/dev/null
>>>>>>> main

# Tests

TEST_TARGETS := test-default test-bench test-short test-verbose test-race
.PHONY: $(TEST_TARGETS) test
<<<<<<< HEAD
test-default: ARGS=-v -race
test-bench:   ARGS=-run=xxxxxMatchNothingxxxxx -test.benchtime=1ms -bench=.
test-short:   ARGS=-short
test-verbose: ARGS=-v
test-race:    ARGS=-race
$(TEST_TARGETS): test
test:
	@set -e; for dir in $(ALL_GO_MOD_DIRS); do \
	  echo "$(GO) test -timeout $(TIMEOUT)s $(ARGS) $${dir}/..."; \
	  (cd "$${dir}" && \
	    $(GO) list ./... \
		  | grep -v third_party \
		  | xargs $(GO) test -timeout $(TIMEOUT)s $(ARGS)); \
	done
=======
test-default test-race: ARGS=-race
test-bench:   ARGS=-run=xxxxxMatchNothingxxxxx -test.benchtime=1ms -bench=.
test-short:   ARGS=-short
test-verbose: ARGS=-v -race
$(TEST_TARGETS): test
test: $(OTEL_GO_MOD_DIRS:%=test/%)
test/%: DIR=$*
test/%:
	@echo "$(GO) test -timeout $(TIMEOUT)s $(ARGS) $(DIR)/..." \
		&& cd $(DIR) \
		&& $(GO) list ./... \
		| grep -v third_party \
		| xargs $(GO) test -timeout $(TIMEOUT)s $(ARGS)
>>>>>>> main

COVERAGE_MODE    = atomic
COVERAGE_PROFILE = coverage.out
.PHONY: test-coverage
test-coverage: | $(GOCOVMERGE)
	@set -e; \
	printf "" > coverage.txt; \
	for dir in $(ALL_COVERAGE_MOD_DIRS); do \
	  echo "$(GO) test -coverpkg=go.opentelemetry.io/otel/... -covermode=$(COVERAGE_MODE) -coverprofile="$(COVERAGE_PROFILE)" $${dir}/..."; \
	  (cd "$${dir}" && \
	    $(GO) list ./... \
	    | grep -v third_party \
	    | xargs $(GO) test -coverpkg=./... -covermode=$(COVERAGE_MODE) -coverprofile="$(COVERAGE_PROFILE)" && \
	  $(GO) tool cover -html=coverage.out -o coverage.html); \
	done; \
	$(GOCOVMERGE) $$(find . -name coverage.out) > coverage.txt

<<<<<<< HEAD
.PHONY: lint
lint: misspell lint-modules | $(GOLANGCI_LINT)
	set -e; for dir in $(ALL_GO_MOD_DIRS); do \
	  echo "golangci-lint in $${dir}"; \
	  (cd "$${dir}" && \
	    $(GOLANGCI_LINT) run --fix && \
	    $(GOLANGCI_LINT) run); \
	done

.PHONY: vanity-import-check
vanity-import-check: | $(PORTO)
	$(PORTO) --include-internal -l .

.PHONY: misspell
misspell: | $(MISSPELL)
	$(MISSPELL) -w $(ALL_DOCS)

.PHONY: lint-modules
lint-modules: | $(CROSSLINK)
	set -e; for dir in $(ALL_GO_MOD_DIRS) $(TOOLS_MOD_DIR); do \
	  echo "$(GO) mod tidy in $${dir}"; \
	  (cd "$${dir}" && \
	    $(GO) mod tidy); \
	done
	echo "cross-linking all go modules"
	$(CROSSLINK)
=======
.PHONY: golangci-lint golangci-lint-fix
golangci-lint-fix: ARGS=--fix
golangci-lint-fix: golangci-lint
golangci-lint: $(OTEL_GO_MOD_DIRS:%=golangci-lint/%)
golangci-lint/%: DIR=$*
golangci-lint/%: | $(GOLANGCI_LINT)
	@echo 'golangci-lint $(if $(ARGS),$(ARGS) ,)$(DIR)' \
		&& cd $(DIR) \
		&& $(GOLANGCI_LINT) run --allow-serial-runners $(ARGS)

.PHONY: crosslink
crosslink: | $(CROSSLINK)
	@echo "cross-linking all go modules" \
		&& $(CROSSLINK)

.PHONY: go-mod-tidy
go-mod-tidy: $(ALL_GO_MOD_DIRS:%=go-mod-tidy/%)
go-mod-tidy/%: DIR=$*
go-mod-tidy/%: | crosslink
	@echo "$(GO) mod tidy in $(DIR)" \
		&& cd $(DIR) \
		&& $(GO) mod tidy

.PHONY: lint-modules
lint-modules: go-mod-tidy

.PHONY: lint
lint: misspell lint-modules golangci-lint

.PHONY: vanity-import-check
vanity-import-check: | $(PORTO)
	@$(PORTO) --include-internal -l .

.PHONY: misspell
misspell: | $(MISSPELL)
	@$(MISSPELL) -w $(ALL_DOCS)
>>>>>>> main

.PHONY: license-check
license-check:
	@licRes=$$(for f in $$(find . -type f \( -iname '*.go' -o -iname '*.sh' \) ! -path '**/third_party/*') ; do \
	           awk '/Copyright The OpenTelemetry Authors|generated|GENERATED/ && NR<=3 { found=1; next } END { if (!found) print FILENAME }' $$f; \
	   done); \
	   if [ -n "$${licRes}" ]; then \
	           echo "license header checking failed:"; echo "$${licRes}"; \
	           exit 1; \
	   fi

<<<<<<< HEAD
=======
DEPENDABOT_PATH=./.github/dependabot.yml
>>>>>>> main
.PHONY: dependabot-check
dependabot-check:
	@result=$$( \
		for f in $$( find . -type f -name go.mod -exec dirname {} \; | sed 's/^.//' ); \
<<<<<<< HEAD
			do grep -q "directory: \+$$f" .github/dependabot.yml \
=======
			do grep -q "directory: \+$$f" $(DEPENDABOT_PATH) \
>>>>>>> main
			|| echo "$$f"; \
		done; \
	); \
	if [ -n "$$result" ]; then \
<<<<<<< HEAD
		echo "missing go.mod dependabot check:"; echo "$$result"; \
		echo "new modules need to be added to the .github/dependabot.yml file"; \
=======
		echo "missing dependabot entry:"; echo "$$result"; \
		echo "new modules need to be added to the $(DEPENDABOT_PATH) file"; \
>>>>>>> main
		exit 1; \
	fi

.PHONY: check-clean-work-tree
check-clean-work-tree:
	@if ! git diff --quiet; then \
	  echo; \
	  echo 'Working tree is not clean, did you forget to run "make precommit"?'; \
	  echo; \
	  git status; \
	  exit 1; \
	fi

.PHONY: prerelease
prerelease: | $(MULTIMOD)
	@[ "${MODSET}" ] || ( echo ">> env var MODSET is not set"; exit 1 )
	$(MULTIMOD) verify && $(MULTIMOD) prerelease -m ${MODSET}

COMMIT ?= "HEAD"
.PHONY: add-tags
add-tags: | $(MULTIMOD)
	@[ "${MODSET}" ] || ( echo ">> env var MODSET is not set"; exit 1 )
	$(MULTIMOD) verify && $(MULTIMOD) tag -m ${MODSET} -c ${COMMIT}
