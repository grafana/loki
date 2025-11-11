CLAUDE.md
=========

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Commands
--------

### Testing

```bash
# Run all tests with race detection (requires MinIO server at localhost:9000)
SERVER_ENDPOINT=localhost:9000 ACCESS_KEY=minioadmin SECRET_KEY=minioadmin ENABLE_HTTPS=1 MINT_MODE=full go test -race -v ./...

# Run tests without race detection
go test ./...

# Run short tests only (no functional tests)
go test -short -race ./...

# Run functional tests
go build -race functional_tests.go
SERVER_ENDPOINT=localhost:9000 ACCESS_KEY=minioadmin SECRET_KEY=minioadmin ENABLE_HTTPS=1 MINT_MODE=full ./functional_tests

# Run functional tests without TLS
SERVER_ENDPOINT=localhost:9000 ACCESS_KEY=minioadmin SECRET_KEY=minioadmin ENABLE_HTTPS=0 MINT_MODE=full ./functional_tests
```

### Linting and Code Quality

```bash
# Run all checks (lint, vet, test, examples, functional tests)
make checks

# Run linter only
make lint

# Run vet and staticcheck
make vet

# Alternative: run golangci-lint directly
golangci-lint run --timeout=5m --config ./.golangci.yml
```

### Building Examples

```bash
# Build all examples
make examples

# Build a specific example
cd examples/s3 && go build -mod=mod putobject.go
```

Architecture
------------

### Core Client Structure

The MinIO Go SDK is organized around a central `Client` struct (api.go:52) that implements Amazon S3 compatible methods. Key architectural patterns:

1.	**Modular API Organization**: API methods are split into logical files:

	-	`api-bucket-*.go`: Bucket operations (lifecycle, encryption, versioning, etc.)
	-	`api-object-*.go`: Object operations (legal hold, retention, tagging, etc.)
	-	`api-get-*.go`, `api-put-*.go`: GET and PUT operations
	-	`api-list.go`: Listing operations
	-	`api-stat.go`: Status/info operations

2.	**Credential Management**: The `pkg/credentials/` package provides various credential providers:

	-	Static credentials
	-	Environment variables (AWS/MinIO)
	-	IAM roles
	-	STS (Security Token Service) variants
	-	File-based credentials
	-	Chain provider for fallback mechanisms

3.	**Request Signing**: The `pkg/signer/` package handles AWS signature versions:

	-	V2 signatures (legacy)
	-	V4 signatures (standard)
	-	Streaming signatures for large uploads

4.	**Transport Layer**: Custom HTTP transport with:

	-	Retry logic with configurable max retries
	-	Health status monitoring
	-	Tracing support via httptrace
	-	Bucket location caching (`bucketLocCache`\)
	-	Session caching for credentials

5.	**Helper Packages**:

	-	`pkg/encrypt/`: Server-side encryption utilities
	-	`pkg/notification/`: Event notification handling
	-	`pkg/policy/`: Bucket policy management
	-	`pkg/lifecycle/`: Object lifecycle rules
	-	`pkg/tags/`: Object and bucket tagging
	-	`pkg/s3utils/`: S3 utility functions
	-	`pkg/kvcache/`: Key-value caching
	-	`pkg/singleflight/`: Deduplication of concurrent requests

### Testing Strategy

-	Unit tests alongside implementation files (`*_test.go`\)
-	Comprehensive functional tests in `functional_tests.go` requiring a live MinIO server
-	Example programs in `examples/` directory demonstrating API usage
-	Build tag `//go:build mint` for integration tests

### Error Handling

-	Custom error types in `api-error-response.go`
-	HTTP status code mapping
-	Retry logic for transient failures
-	Detailed error context preservation

Important Patterns
------------------

1.	**Context Usage**: All API methods accept `context.Context` for cancellation and timeout control
2.	**Options Pattern**: Methods use Options structs for optional parameters (e.g., `PutObjectOptions`, `GetObjectOptions`\)
3.	**Streaming Support**: Large file operations use io.Reader/Writer interfaces for memory efficiency
4.	**Bucket Lookup Types**: Supports both path-style and virtual-host-style S3 URLs
5.	**MD5/SHA256 Hashing**: Configurable hash functions for integrity checks via `md5Hasher` and `sha256Hasher`
