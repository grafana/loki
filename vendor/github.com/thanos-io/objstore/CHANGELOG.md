# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/) and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

NOTE: As semantic versioning states all 0.y.z releases can contain breaking changes in API (flags, grpc API, any backward compatibility)

We use *breaking :warning:* to mark changes that are not backward compatible (relates only to v0.y.z releases.)

## Unreleased

### Fixed
- [#33](https://github.com/thanos-io/objstore/pull/33) Tracing: Add `ContextWithTracer()` to inject the tracer into the context.
- [#34](https://github.com/thanos-io/objstore/pull/34) Fix ignored options when creating shared credential Azure client.

### Added
- [#15](https://github.com/thanos-io/objstore/pull/15) Add Oracle Cloud Infrastructure Object Storage Bucket support.
- [#25](https://github.com/thanos-io/objstore/pull/25) S3: Support specifying S3 storage class.
- [#32](https://github.com/thanos-io/objstore/pull/32) Swift: Support authentication using application credentials.
- [#41](https://github.com/thanos-io/objstore/pull/41) S3: Support S3 session token.
- [#43](https://github.com/thanos-io/objstore/pull/43) filesystem: abort filesystem bucket operations if the context has been cancelled

### Changed
- [#38](https://github.com/thanos-io/objstore/pull/38) *: Upgrade minio-go version to `v7.0.45`.
- [#39](https://github.com/thanos-io/objstore/pull/39) COS: Upgrade cos sdk version to `v0.7.40`.
- [#35](https://github.com/thanos-io/objstore/pull/35) Azure: Update Azure SDK and fix breaking changes.

### Removed
