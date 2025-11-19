default: fmt get update test lint

GO       := go
GOBIN    := $(shell pwd)/bin
GOBUILD  := CGO_ENABLED=0 $(GO) build $(BUILD_FLAG)

GOTESTSUM         := $(GOBIN)/gotestsum
# renovate: datasource=github-releases depName=gotestyourself/gotestsum
GOTESTSUM_VERSION := v1.13.0
$(GOTESTSUM):
	GOBIN=$(GOBIN) go install gotest.tools/gotestsum@$(GOTESTSUM_VERSION)

TESTSTAT         := $(GOBIN)/teststat
# renovate: datasource=github-releases depName=vearutop/teststat
TESTSTAT_VERSION := v0.1.27
$(TESTSTAT):
	GOBIN=$(GOBIN) go install github.com/vearutop/teststat@$(TESTSTAT_VERSION)

FILES := $(shell find . -name '*.go' -type f -not -name '*.pb.go' -not -name '*_generated.go' -not -name '*_test.go')
TESTS := $(shell find . -name '*.go' -type f -not -name '*.pb.go' -not -name '*_generated.go' -name '*_test.go')

get:
	$(GO) get ./...
	$(GO) mod verify
	$(GO) mod tidy

update:
	$(GO) get -u -v ./...
	$(GO) mod verify
	$(GO) mod tidy

fmt:
	gofmt -s -l -w $(FILES) $(TESTS)

lint:
	GOFLAGS="-tags=functional" golangci-lint run

test: $(GOTESTSUM) $(TESTSTAT) $(TPARSE)
	@$(GOTESTSUM) $(if ${CI},--format github-actions,--format testdox) --jsonfile _test/unittests.json --junitfile _test/unittests.xml \
		--rerun-fails --packages="./..." \
		-- -v -race -coverprofile=profile.out -covermode=atomic -timeout 2m
	@$(TESTSTAT) _test/unittests.json

.PHONY: test_functional
test_functional: $(GOTESTSUM) $(TESTSTAT) $(TPARSE)
	@$(GOTESTSUM) $(if ${CI},--format github-actions,--format testdox) --jsonfile _test/fvt.json --junitfile _test/fvt.xml \
		--rerun-fails --packages="./..." \
		-- -v -race -coverprofile=profile.out -covermode=atomic -timeout 15m -tags=functional
	@$(TESTSTAT) _test/fvt.json
