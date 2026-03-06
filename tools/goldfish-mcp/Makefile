.PHONY: build clean test lint help

# Binary name
BINARY_NAME=goldfish-mcp

# Build the binary
build:
	go build -o $(BINARY_NAME) .

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	go clean

# Run tests
test:
	go test ./...

# Run linter
lint:
	golangci-lint run

# Display help
help:
	@echo "Available targets:"
	@echo "  build  - Build the goldfish-mcp binary"
	@echo "  clean  - Remove build artifacts"
	@echo "  test   - Run tests"
	@echo "  lint   - Run linter"
	@echo "  help   - Display this help message"
