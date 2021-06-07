PACKAGE  = github.com/devigned/tab
DATE    ?= $(shell date +%FT%T%z)
VERSION ?= $(shell git describe --tags --always --dirty --match=v* 2> /dev/null || \
			cat $(CURDIR)/.version 2> /dev/null || echo v0)
BIN      = $(GOPATH)/bin
BASE     = $(CURDIR)
PKGS     = $(or $(PKG),$(shell cd $(BASE) && env GOPATH=$(GOPATH) $(GO) list ./... | grep -vE "^$(PACKAGE)/templates/"))
TESTPKGS = $(shell env GOPATH=$(GOPATH) $(GO) list -f '{{ if or .TestGoFiles .XTestGoFiles }}{{ .ImportPath }}{{ end }}' $(PKGS))
GO_FILES = find . -iname '*.go' -type f

GO      = go
GODOC   = godoc
GOFMT   = gofmt
GOCYCLO = gocyclo

V = 0
Q = $(if $(filter 1,$V),,@)
M = $(shell printf "\033[34;1m▶\033[0m")
TIMEOUT = 1100

.PHONY: all
all: fmt lint vet ; $(info $(M) building library…) @ ## Build program
	$Q cd $(BASE) && $(GO) build -tags release

# Tools

GOLINT = $(BIN)/golint
$(BIN)/golint: ; $(info $(M) building golint…)
	$Q go get github.com/golang/lint/golint

# Tests

TEST_TARGETS := test-default test-bench test-verbose test-race test-debug test-cover
.PHONY: $(TEST_TARGETS) test-xml check test tests
test-bench:   ARGS=-run=__absolutelynothing__ -bench=. 		## Run benchmarks
test-verbose: ARGS=-v            							## Run tests in verbose mode
test-debug:   ARGS=-v -debug     							## Run tests in verbose mode with debug output
test-race:    ARGS=-race         							## Run tests with race detector
test-cover:   ARGS=-cover -coverprofile=cover.out -v     	## Run tests in verbose mode with coverage
$(TEST_TARGETS): NAME=$(MAKECMDGOALS:test-%=%)
$(TEST_TARGETS): test
check test tests: cyclo lint vet; $(info $(M) running $(NAME:%=% )tests…) @ ## Run tests
	$Q cd $(BASE) && $(GO) test -timeout $(TIMEOUT)s $(ARGS) $(TESTPKGS)

.PHONY: vet
vet: $(GOLINT) ; $(info $(M) running vet…) @ ## Run vet
	$Q cd $(BASE) && $(GO) vet ./...

.PHONY: lint
lint: $(GOLINT) ; $(info $(M) running golint…) @ ## Run golint
	$Q cd $(BASE) && ret=0 && for pkg in $(PKGS); do \
		test -z "$$($(GOLINT) $$pkg | tee /dev/stderr)" || ret=1 ; \
	 done ; exit $$ret

.PHONY: fmt
fmt: ; $(info $(M) running gofmt…) @ ## Run gofmt on all source files
	@ret=0 && for d in $$($(GO) list -f '{{.Dir}}' ./...); do \
		$(GOFMT) -l -w $$d/*.go || ret=$$? ; \
	 done ; exit $$ret

.PHONY: cyclo
cyclo: ; $(info $(M) running gocyclo...) @ ## Run gocyclo on all source files
	$Q cd $(BASE) && $(GOCYCLO) -over 19 $$($(GO_FILES))

.Phony: destroy-sb
destroy-sb: ; $(info $(M) running sb destroy...)
	$(Q) terraform destroy -auto-approve

# Dependency management
go.sum: go.mod
	$Q cd $(BASE) && $(GO) mod tidy

# Misc

.PHONY: clean
clean: ; $(info $(M) cleaning…)	@ ## Cleanup everything
	@rm -rf test/tests.* test/coverage.*

.PHONY: help
help:
	@grep -E '^[ a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.PHONY: version
version:
	@echo $(VERSION)