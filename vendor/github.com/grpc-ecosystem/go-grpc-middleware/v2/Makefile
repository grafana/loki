include .bingo/Variables.mk

SHELL=/bin/bash

PROVIDER_MODULES ?= $(shell ls -d $(PWD)/providers/*)
MODULES          ?= $(PROVIDER_MODULES) $(PWD)/

GOBIN             ?= $(firstword $(subst :, ,${GOPATH}))/bin

PROTOC_VERSION    ?= 3.12.3
PROTOC            ?= $(GOBIN)/protoc-$(PROTOC_VERSION)
TMP_GOPATH        ?= /tmp/gopath


GO111MODULE       ?= on
export GO111MODULE
GOPROXY           ?= https://proxy.golang.org
export GOPROXY

define require_clean_work_tree
	@git update-index -q --ignore-submodules --refresh

    @if ! git diff-files --quiet --ignore-submodules --; then \
        echo >&2 "cannot $1: you have unstaged changes."; \
        git diff-files --name-status -r --ignore-submodules -- >&2; \
        echo >&2 "Please commit or stash them."; \
        exit 1; \
    fi

    @if ! git diff-index --cached --quiet HEAD --ignore-submodules --; then \
        echo >&2 "cannot $1: your index contains uncommitted changes."; \
        git diff-index --cached --name-status -r --ignore-submodules HEAD -- >&2; \
        echo >&2 "Please commit or stash them."; \
        exit 1; \
    fi

endef

all: fmt proto lint test

.PHONY: fmt
fmt: $(GOIMPORTS)
	@echo "Running fmt for all modules: $(MODULES)"
	@$(GOIMPORTS) -local github.com/grpc-ecosystem/go-grpc-middleware/v2 -w $(MODULES)

.PHONY: proto
proto: ## Generates Go files from Thanos proto files.
proto: $(GOIMPORTS) $(PROTOC) $(PROTOC_GEN_GOGOFAST) ./grpctesting/testpb/test.proto
	@GOIMPORTS_BIN="$(GOIMPORTS)" PROTOC_BIN="$(PROTOC)" PROTOC_GEN_GOGOFAST_BIN="$(PROTOC_GEN_GOGOFAST)" scripts/genproto.sh

.PHONY: test
test:
	@echo "Running tests for all modules: $(MODULES)"
	for dir in $(MODULES) ; do \
		$(MAKE) test_module DIR=$${dir} ; \
	done
	./scripts/test_all.sh

.PHONY: test_module
test_module:
	@echo "Running tests for dir: $(DIR)"
	cd $(DIR) && go test -v -race ./...

.PHONY: lint
# PROTIP:
# Add
#      --cpu-profile-path string   Path to CPU profile output file
#      --mem-profile-path string   Path to memory profile output file
# to debug big allocations during linting.
lint: ## Runs various static analysis tools against our code.
lint: fmt $(FAILLINT) $(GOLANGCI_LINT) $(MISSPELL)
	@echo "Running lint for all modules: $(MODULES)"
	./scripts/git-tree.sh
	@echo ">> verifying modules being imported"
	@$(FAILLINT) -paths "errors=github.com/pkg/errors,fmt.{Print,Printf,Println}" ./...
	@echo ">> examining all of the Go files"
	@go vet -stdmethods=false ./...
	@echo ">> linting all of the Go files GOGC=${GOGC}"
	@$(GOLANGCI_LINT) run
	@echo ">> detecting misspells"
	@find . -type f | grep -v vendor/ | grep -vE '\./\..*' | xargs $(MISSPELL) -error
	@echo ">> ensuring generated proto files are up to date"
	@$(MAKE) proto
	./scripts/git-tree.sh
$(PROTOC):
	@mkdir -p $(TMP_GOPATH)
	@echo ">> fetching protoc@${PROTOC_VERSION}"
	@PROTOC_VERSION="$(PROTOC_VERSION)" TMP_GOPATH="$(TMP_GOPATH)" scripts/installprotoc.sh
	@echo ">> installing protoc@${PROTOC_VERSION}"
	@mv -- "$(TMP_GOPATH)/bin/protoc" "$(GOBIN)/protoc-$(PROTOC_VERSION)"
	@echo ">> produced $(GOBIN)/protoc-$(PROTOC_VERSION)"
