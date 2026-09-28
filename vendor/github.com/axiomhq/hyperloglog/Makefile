GO_FILES := $(shell git ls-files '*.go')
GOLANGCI_LINT_VERSION := $(shell cat .golangci-lint-version)
TOOLS_DIR := $(CURDIR)/.tools
GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint

.PHONY: ci fmt fmt-check lint test test-race coverage build clean

ci: fmt-check lint test-race build

fmt:
	gofmt -w $(GO_FILES)

fmt-check:
	@unformatted="$$(gofmt -l $(GO_FILES))"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

$(GOLANGCI_LINT): .golangci-lint-version
	@mkdir -p $(TOOLS_DIR)
	GOBIN=$(TOOLS_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

test:
	go test ./...

test-race:
	go test -race -shuffle=on ./...

coverage:
	go test -race -shuffle=on -covermode=atomic -coverprofile=coverage.out ./...

build:
	go build ./...

clean:
	go clean
	rm -f coverage.out
