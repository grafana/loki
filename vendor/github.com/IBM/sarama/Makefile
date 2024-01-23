default: fmt get update test lint

GO       := go
GOBIN    := $(shell pwd)/bin
GOBUILD  := CGO_ENABLED=0 $(GO) build $(BUILD_FLAG)
GOTEST   := $(GO) test -v -race -coverprofile=profile.out -covermode=atomic

FILES    := $(shell find . -name '*.go' -type f -not -name '*.pb.go' -not -name '*_generated.go' -not -name '*_test.go')
TESTS    := $(shell find . -name '*.go' -type f -not -name '*.pb.go' -not -name '*_generated.go' -name '*_test.go')

$(GOBIN)/tparse:
	GOBIN=$(GOBIN) go install github.com/mfridman/tparse@v0.11.1
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

test: $(GOBIN)/tparse
	$(GOTEST) -timeout 2m -json ./... \
		| tee output.json | $(GOBIN)/tparse -follow -all
	[ -z "$${GITHUB_STEP_SUMMARY}" ] \
		|| NO_COLOR=1 $(GOBIN)/tparse -format markdown -file output.json -all >"$${GITHUB_STEP_SUMMARY:-/dev/null}"
.PHONY: test_functional
test_functional: $(GOBIN)/tparse
	$(GOTEST) -timeout 15m -tags=functional -json ./... \
		| tee output.json | $(GOBIN)/tparse -follow -all
	[ -z "$${GITHUB_STEP_SUMMARY:-}" ] \
		|| NO_COLOR=1 $(GOBIN)/tparse -format markdown -file output.json -all >"$${GITHUB_STEP_SUMMARY:-/dev/null}"
