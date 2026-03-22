# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go library implementing SCRAM (Salted Challenge Response Authentication Mechanism) per RFC-5802 and RFC-7677. It provides both client and server implementations supporting SHA-1, SHA-256, and SHA-512 hash functions.

## Development Commands

**Run tests:**
```bash
go test ./...
```

**Run tests with race detection (CI configuration):**
```bash
go test -race ./...
```

**Build (module-only, no binaries):**
```bash
go build ./...
```

**Run single test:**
```bash
go test -run TestName ./...
```

## Architecture

### Core Components

**Hash factory pattern:** The `HashGeneratorFcn` type (scram.go:23) is the entry point for creating clients and servers. Package-level variables `SHA1`, `SHA256`, `SHA512` provide pre-configured hash functions. All client/server creation flows through these hash generators.

**Client (`client.go`):** Holds authentication configuration for a username/password/authzID tuple. Maintains a cache of derived keys (PBKDF2 results) indexed by `KeyFactors` (salt + iteration count). Thread-safe via RWMutex. Creates `ClientConversation` instances for individual auth attempts.

**Server (`server.go`):** Holds credential lookup callback and nonce generator. Creates `ServerConversation` instances for individual auth attempts.

**Conversations:** State machines implementing the SCRAM protocol exchange:
- `ClientConversation` (client_conv.go): States are `clientStarting` → `clientFirst` → `clientFinal` → `clientDone`
- `ServerConversation` (server_conv.go): States are `serverFirst` → `serverFinal` → `serverDone`

Both use a `Step(string) (string, error)` method to advance through protocol stages.

**Message parsing (`parse.go`):** Parses SCRAM protocol messages into structs. Separate parsers for client-first, server-first, client-final, and server-final messages.

**Shared utilities (`common.go`):**
- `NonceGeneratorFcn`: Default uses base64-encoded 24 bytes from crypto/rand
- `derivedKeys`: Struct caching ClientKey, StoredKey, ServerKey
- `KeyFactors`: Salt + iteration count, used as cache key
- `StoredCredentials`: What servers must store for each user
- `CredentialLookup`: Callback type servers use to retrieve stored credentials

### Key Design Patterns

**Dependency injection:** Server requires a `CredentialLookup` callback, making storage mechanism pluggable.

**Caching:** Client caches expensive PBKDF2 results in a map keyed by `KeyFactors`. This optimizes reconnection scenarios where salt/iteration count remain constant.

**Factory methods:** Hash generators provide `.NewClient()` and `.NewServer()` methods that handle SASLprep normalization. Alternative `.NewClientUnprepped()` exists for custom normalization.

**Configuration via chaining:** Both Client and Server support `.WithNonceGenerator()` for custom nonce generation (primarily for testing).

### Security Considerations

- Default minimum PBKDF2 iterations: 4096 (client.go:45)
- All string comparisons use `hmac.Equal()` for constant-time comparison
- SASLprep normalization applied by default via xdg-go/stringprep dependency
- Nonce generation uses crypto/rand

## Testing

Tests include conversation state machine tests (client_conv_test.go, server_conv_test.go), integration tests, and examples (doc_test.go). Test data in testdata_test.go.
