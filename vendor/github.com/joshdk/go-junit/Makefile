#### Environment ####

# Enforce usage of the Go modules system.
export GO111MODULE := on

# Determine where `go get` will install binaries to.
GOBIN := $(HOME)/go/bin
ifdef GOPATH
	GOBIN := $(GOPATH)/bin
endif

# All target for when make is run on its own.
.PHONY: all
all: test lint

#### Binary Dependencies ####

# Install binary for go-junit-report.
go-junit-report := $(GOBIN)/go-junit-report
$(go-junit-report):
	cd /tmp && go get -u github.com/jstemmer/go-junit-report

# Install binary for golangci-lint.
golangci-lint := $(GOBIN)/golangci-lint
$(golangci-lint):
	@./scripts/install-golangci-lint $(golangci-lint)

# Install binary for goimports.
goimports := $(GOBIN)/goimports
$(goimports):
	cd /tmp && go get -u golang.org/x/tools/cmd/goimports

#### Linting ####

# Run code linters.
.PHONY: lint
lint: $(golangci-lint) style
	golangci-lint run

# Run code formatters. Unformatted code will fail in CircleCI.
.PHONY: style
style: $(goimports)
ifdef CI
	goimports -l .
else
	goimports -l -w .
endif

#### Testing ####

# Run Go tests and generate a JUnit XML style test report for ingestion by CircleCI.
.PHONY: test
test: $(go-junit-report)
	@mkdir -p test-results
	@go test -race -v 2>&1 | tee test-results/report.log
	@cat test-results/report.log | go-junit-report -set-exit-code > test-results/report.xml

# Clean up test reports.
.PHONY: clean
clean:
	@rm -rf test-results
