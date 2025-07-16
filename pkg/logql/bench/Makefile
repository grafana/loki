.PHONY: generate bench list run run-debug stream

# Default size for data generation (2GB)
SIZE ?= 2147483648
TENANT ?= test-tenant
GO_BUILD_FLAGS := -tags=slicelabels

generate:
	@echo "Generating benchmark data..."
	@go run $(GO_BUILD_FLAGS) ./cmd/generate/main.go -size $(SIZE) -tenant $(TENANT)

bench:
	@echo "Running all benchmarks..."
	@go test $(GO_BUILD_FLAGS) -bench=. -benchmem -count=1 -timeout=1h

list:
	@go run $(GO_BUILD_FLAGS) ./cmd/bench/main.go list

run:
	@go run $(GO_BUILD_FLAGS) ./cmd/bench/main.go run

run-debug:
	@DEBUG=1 go run $(GO_BUILD_FLAGS) ./cmd/bench/main.go run

stream:
	@go run $(GO_BUILD_FLAGS) ./cmd/stream/main.go | less
