.PHONY: generate bench list run run-debug stream

# Default size for data generation (1GB)
SIZE ?= 1073741824
TENANT ?= test-tenant

generate:
	@echo "Generating benchmark data..."
	@go run ./cmd/generate/main.go -size $(SIZE) -tenant $(TENANT)

bench:
	@echo "Running all benchmarks..."
	@go test -bench=. -benchmem -count=1 -timeout=1h

list:
	@go run ./cmd/bench/main.go list

run:
	@go run ./cmd/bench/main.go run

run-debug:
	@DEBUG=1 go run ./cmd/bench/main.go run

stream:
	@go run ./cmd/stream/main.go | less
