# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/) and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

NOTE: As semantic versioning states all 0.y.z releases can contain breaking changes in API (flags, grpc API, any backward compatibility)

We use *breaking :warning:* to mark changes that are not backward compatible (relates only to v0.y.z releases.)

## Unreleased

### Fixed
- [#117](https://github.com/thanos-io/objstore/pull/117) Metrics: Fix `objstore_bucket_operation_failures_total` incorrectly incremented if context is cancelled while reading object contents.
- [#115](https://github.com/thanos-io/objstore/pull/115) GCS: Fix creation of bucket with GRPC connections. Also update storage client to `v1.40.0`.
- [#102](https://github.com/thanos-io/objstore/pull/102) Azure: bump azblob sdk to get concurrency fixes.
- [#33](https://github.com/thanos-io/objstore/pull/33) Tracing: Add `ContextWithTracer()` to inject the tracer into the context.
- [#34](https://github.com/thanos-io/objstore/pull/34) Fix ignored options when creating shared credential Azure client.
- [#62](https://github.com/thanos-io/objstore/pull/62) S3: Fix ignored context cancellation in `Iter` method.
- [#77](https://github.com/thanos-io/objstore/pull/77) Fix buckets wrapped with metrics from being unable to determine object sizes in `Upload`.
- [#78](https://github.com/thanos-io/objstore/pull/78) S3: Fix possible concurrent modification of the PutUserMetadata map.
- [#79](https://github.com/thanos-io/objstore/pull/79) Metrics: Fix `objstore_bucket_operation_duration_seconds` for `iter` operations.

### Added
- [#15](https://github.com/thanos-io/objstore/pull/15) Add Oracle Cloud Infrastructure Object Storage Bucket support.
- [#25](https://github.com/thanos-io/objstore/pull/25) S3: Support specifying S3 storage class.
- [#32](https://github.com/thanos-io/objstore/pull/32) Swift: Support authentication using application credentials.
- [#41](https://github.com/thanos-io/objstore/pull/41) S3: Support S3 session token.
- [#43](https://github.com/thanos-io/objstore/pull/43) filesystem: abort filesystem bucket operations if the context has been cancelled
- [#44](https://github.com/thanos-io/objstore/pull/44) Add new metric to count total number of fetched bytes from bucket
- [#50](https://github.com/thanos-io/objstore/pull/50) Add Huawei Cloud OBS Object Storage Support
- [#59](https://github.com/thanos-io/objstore/pull/59) Adding method `IsCustomerManagedKeyError` on the bucket interface.
- [#61](https://github.com/thanos-io/objstore/pull/61) Add OpenTelemetry TracingBucket.
    > This also changes the behaviour of `client.NewBucket`. Now it returns, uninstrumented and untraced bucket.
    You can combine `objstore.WrapWithMetrics` and `tracing/{opentelemetry,opentracing}.WrapWithTraces` to have old behavior.
- [#69](https://github.com/thanos-io/objstore/pull/69) [#66](https://github.com/thanos-io/objstore/pull/66) Add `objstore_bucket_operation_transferred_bytes` that counts the number of total bytes read from the bucket operation Get/GetRange and also counts the number of total bytes written to the bucket operation Upload.
- [#64](https://github.com/thanos-io/objstore/pull/64) OCI: OKE Workload Identity support.
- [#73](https://github.com/thanos-io/objstore/pull/73) Аdded file path to erros from DownloadFile
- [#51](https://github.com/thanos-io/objstore/pull/51) Azure: Support using connection string authentication.
- [#76](https://github.com/thanos-io/objstore/pull/76) GCS: Query for object names only in `Iter` to possibly improve performance when listing objects.
- [#85](https://github.com/thanos-io/objstore/pull/85) S3: Allow checksum algorithm to be configured
- [#92](https://github.com/thanos-io/objstore/pull/92) GCS: Allow using a gRPC client.
- [#94](https://github.com/thanos-io/objstore/pull/94) Allow timingReadCloser to be seeker
- [#96](https://github.com/thanos-io/objstore/pull/96) Allow nopCloserWithObjectSize to be seeker
- [#86](https://github.com/thanos-io/objstore/pull/86) GCS: Add HTTP Config to GCS
- [#99](https://github.com/thanos-io/objstore/pull/99) Swift: Add HTTP_Config
- [#108](https://github.com/thanos-io/objstore/pull/108) Metrics: Add native histogram definitions to histograms
- [#112](https://github.com/thanos-io/objstore/pull/112) S3: Add `DisableDualstack option.
- [#100](https://github.com/thanos-io/objstore/pull/100) s3: add DisableMultipart option
- [#116](https://github.com/thanos-io/objstore/pull/116) Azure: Add new storage_create_container configuration property
- [#128](https://github.com/thanos-io/objstore/pull/128) GCS: Add support for `ChunkSize` for writer.
- [#130](https://github.com/thanos-io/objstore/pull/130) feat: Decouple creating bucket metrics from instrumenting the bucket

### Changed
- [#38](https://github.com/thanos-io/objstore/pull/38) *: Upgrade minio-go version to `v7.0.45`.
- [#39](https://github.com/thanos-io/objstore/pull/39) COS: Upgrade cos sdk version to `v0.7.40`.
- [#35](https://github.com/thanos-io/objstore/pull/35) Azure: Update Azure SDK and fix breaking changes.
- [#65](https://github.com/thanos-io/objstore/pull/65) *: Upgrade minio-go version to `v7.0.61`.
- [#70](https://github.com/thanos-io/objstore/pull/70) GCS: Update cloud.google.com/go/storage version to `v1.27.0`.
- [#71](https://github.com/thanos-io/objstore/pull/71) Replace method `IsCustomerManagedKeyError` for a more generic `IsAccessDeniedErr` on the bucket interface.
- [#89](https://github.com/thanos-io/objstore/pull/89) GCS: Upgrade cloud.google.com/go/storage version to `v1.35.1`.
- [#123](https://github.com/thanos-io/objstore/pull/123) *: Upgrade minio-go version to `v7.0.71`.
### Removed
