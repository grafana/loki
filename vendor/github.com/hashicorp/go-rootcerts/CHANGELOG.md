## Unreleased

### Improvements

### Changes

### Fixed

### Security

## v1.1.0 (2026-10-23)

### Changes
- Minimum Go version raised from 1.12 to 1.26 (go.mod).
- Added copyright and license headers.
- CI: Go 1.26.10 toolchain, golangci-lint v2.14.0, GitHub Actions updates.

### Security
- CI toolchain updated to fix GO-2025-3503 (net/http NO_PROXY bypass). The library does not import net/http.
