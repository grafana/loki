# Changelog

## Version 1.50.2 (2026-06-05)

## What's Changed
### :tada: New Features / Improvements
* feat(consumer): add support for SyncGroupRequest/Response v5 (KIP-559) by @dnwe in https://github.com/IBM/sarama/pull/3591
* feat(txn): add protocol support for TxnOffsetCommit v3 by @dnwe in https://github.com/IBM/sarama/pull/3592
* feat(txn): support consumer group metadata in TxnOffsetCommit v3 by @dnwe in https://github.com/IBM/sarama/pull/3593
* feat(admin): add protocol support for DeleteRecords v2 (KIP-482) by @dnwe in https://github.com/IBM/sarama/pull/3594
* feat(protocol): add support for DescribeConfigs v3 and v4 by @dnwe in https://github.com/IBM/sarama/pull/3596
* feat(admin): add DescribeConfigs for multiple resources by @dnwe in https://github.com/IBM/sarama/pull/3600
* feat(consumer): option to cap decompressed batch size by @dnwe in https://github.com/IBM/sarama/pull/3604
### :bug: Fixes
* fix(admin): retry ACL and SCRAM ops on stale controller by @dnwe in https://github.com/IBM/sarama/pull/3598
### :package: Dependency updates
* fix(deps): update module github.com/pierrec/lz4/v4 to v4.1.27 by @renovate[bot] in https://github.com/IBM/sarama/pull/3597

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.50.1...v1.50.2

## Version 1.50.1 (2026-05-27)

## What's Changed
### :bug: Fixes
* fix: correct requiredVersion for V8 JoinGroup and add protocol version placeholders by @dnwe in https://github.com/IBM/sarama/pull/3585

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.50.0...v1.50.1

## Version 1.50.0 (2026-05-27)

## What's Changed
### :tada: New Features / Improvements
* feat: add Java-compatible murmur2 partitioner by @dnwe in https://github.com/IBM/sarama/pull/3567
* feat(protocol): support LeaveGroupRequest/Response v5 (KIP-800) by @dnwe in https://github.com/IBM/sarama/pull/3576
* feat(protocol): support JoinGroupRequest/Response v8 (KIP-800) by @dnwe in https://github.com/IBM/sarama/pull/3577
* feat(consumer_group): set cancellation cause on session context by @prakhar7651 in https://github.com/IBM/sarama/pull/3575
* feat(consumer_group): send KIP-800 reason on JoinGroup and LeaveGroup by @dnwe in https://github.com/IBM/sarama/pull/3584
* feat: add Kafka 4.3.0 version placeholder by @dnwe in https://github.com/IBM/sarama/pull/3587
* feat(admin): support OffsetFetchRequest v8 by @dnwe in https://github.com/IBM/sarama/pull/3565
### :bug: Fixes
* fix(client): don't log ErrNoTopicsToUpdateMetadata every tick by @dnwe in https://github.com/IBM/sarama/pull/3566
* fix(broker): snapshot fetch meters before deferred Mark by @dnwe in https://github.com/IBM/sarama/pull/3563
* fix: prevent len out of range panic on 32bit architectures by @gibmat in https://github.com/IBM/sarama/pull/3579
* fix(offset): retry fetchInitialOffset on top-level coordinator errors by @dnwe in https://github.com/IBM/sarama/pull/3574
### :package: Dependency updates
* chore(deps): update docker/bake-action action to v7.2.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3568
* fix(deps): update module golang.org/x/sys to v0.45.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3570
* chore(deps): update golangci/golangci-lint-action action to v9.2.1 by @renovate[bot] in https://github.com/IBM/sarama/pull/3571
* chore(deps): update module golang.org/x/crypto to v0.52.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3572
* fix(deps): update module golang.org/x/net to v0.55.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3573
* chore(deps): update docker/setup-buildx-action action to v4.1.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3578
### :wrench: Maintenance
* refactor: replace eapache/queue with generic ring buffer by @dnwe in https://github.com/IBM/sarama/pull/3560
* test(fvt): use a per-message timeout in follower failover test by @dnwe in https://github.com/IBM/sarama/pull/3562

## New Contributors
* @gibmat made their first contribution in https://github.com/IBM/sarama/pull/3579
* @prakhar7651 made their first contribution in https://github.com/IBM/sarama/pull/3575

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.49.0...v1.50.0

## Version 1.49.0 (2026-05-18)

## What's Changed
### :rotating_light: Breaking Changes
* fix(consumer): decouple FetchRequest.MaxBytes from MaxResponseSize by @dnwe in https://github.com/IBM/sarama/pull/3538
### :tada: New Features / Improvements
* feat(consumer): warn on sustained partition retries by @dnwe in https://github.com/IBM/sarama/pull/3535
* feat(producer): add Produce v8 request/response support by @dnwe in https://github.com/IBM/sarama/pull/3540
* feat(consumer): cap partition consumer retries by @dnwe in https://github.com/IBM/sarama/pull/3539
* feat: support FindCoordinator V3 protocol by @hindessm in https://github.com/IBM/sarama/pull/3544
* feat: support describe acls v2 by @hindessm in https://github.com/IBM/sarama/pull/3548
* feat: support create acls v2 by @hindessm in https://github.com/IBM/sarama/pull/3549
* feat: support delete acls v2 by @hindessm in https://github.com/IBM/sarama/pull/3550
* feat: support sasl authenticate v2 by @hindessm in https://github.com/IBM/sarama/pull/3551
* feat: support create partitions v2 by @hindessm in https://github.com/IBM/sarama/pull/3554
* feat: support join group v7 by @hindessm in https://github.com/IBM/sarama/pull/3555
### :bug: Fixes
* fix: flexible decoder out-of-bounds panic by @hindessm in https://github.com/IBM/sarama/pull/3543
* fix(consumer): size partial-batch retry correctly by @dnwe in https://github.com/IBM/sarama/pull/3541
* feat(consumer): add OffsetCommit v8 request/response support by @dnwe in https://github.com/IBM/sarama/pull/3545
* fix: decode nullable ACL describe error messages by @dnwe in https://github.com/IBM/sarama/pull/3552
* fix(consumer): lease preferred read replicas by @dnwe in https://github.com/IBM/sarama/pull/3553
* fix(producer): honour Retry.Backoff in idempotent retryBatch by @dnwe in https://github.com/IBM/sarama/pull/3557
### :wrench: Maintenance
* chore: better bounds checking by @hindessm in https://github.com/IBM/sarama/pull/3546
* chore: bump deps in ./examples tree by @dnwe in https://github.com/IBM/sarama/pull/3558
* docs: add AlterPartitionReassignments example and functional test by @dnwe in https://github.com/IBM/sarama/pull/3556


**Full Changelog**: https://github.com/IBM/sarama/compare/v1.48.2...v1.49.0

## Version 1.48.2 (2026-05-13)

## What's Changed
### :tada: New Features / Improvements
* feat(admin): add KIP-396 list/alter offsets APIs by @DCjanus in https://github.com/IBM/sarama/pull/3419
* feat: add SubscriptionUserDataProvider hook for BalanceStrategy by @lizthegrey in https://github.com/IBM/sarama/pull/3506
* perf(zstd): scale idle zstd encoder cap to GOMAXPROCS by @lizthegrey in https://github.com/IBM/sarama/pull/3507
### :bug: Fixes
* fix: retry ListTopics on transient transport errors by @huynhanx03 in https://github.com/IBM/sarama/pull/3497
* test(fvt): only safeClose if we created by @dnwe in https://github.com/IBM/sarama/pull/3530
* fix(client): scope metadata refresh errors to requested topics by @dnwe in https://github.com/IBM/sarama/pull/3532
### :wrench: Maintenance
* test(fvt): speedup functional test runs by @dnwe in https://github.com/IBM/sarama/pull/3528


**Full Changelog**: https://github.com/IBM/sarama/compare/v1.48.1...v1.48.2

## Version 1.48.1 (2026-05-10)

## What's Changed
### :bug: Fixes
* perf: cache topic batch-size metric lookup by @huynhanx03 in https://github.com/IBM/sarama/pull/3498
* fix: stabilise TestFuncTxnProduceAndCommitOffset flakes by @dnwe in https://github.com/IBM/sarama/pull/3517
* test: relax producer batch metrics assertions by @DCjanus in https://github.com/IBM/sarama/pull/3523
* fix: prevent race during partition consumer close by @dnwe in https://github.com/IBM/sarama/pull/3524
* fix: return leaderless errors in metadata refresh by @dnwe in https://github.com/IBM/sarama/pull/3525
### :package: Dependency updates
* chore(deps): update dependency golangci/golangci-lint to v2.12.1 by @renovate[bot] in https://github.com/IBM/sarama/pull/3509
* chore(deps): bump github.com/klauspost/compress from 1.18.5 to 1.18.6 by @dependabot[bot] in https://github.com/IBM/sarama/pull/3508
* chore(deps): bump golang.org/x/sys from 0.43.0 to 0.44.0 in the golang-x group across 1 directory by @dependabot[bot] in https://github.com/IBM/sarama/pull/3520
* chore(deps): update module golang.org/x/crypto to v0.51.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3521
* fix(deps): update module golang.org/x/net to v0.54.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3526
* chore(deps): update dependency golangci/golangci-lint to v2.12.2 by @renovate[bot] in https://github.com/IBM/sarama/pull/3515
### :wrench: Maintenance
* chore: add testifylint and fix lint warnings by @dnwe in https://github.com/IBM/sarama/pull/3522

## New Contributors
* @huynhanx03 made their first contribution in https://github.com/IBM/sarama/pull/3498

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.48.0...v1.48.1

## Version 1.48.0 (2026-04-24)

## What's Changed
### :tada: New Features / Improvements
* feat(producer): partition muting for msg ordering by @dnwe in https://github.com/IBM/sarama/pull/3422
### :bug: Fixes
* fix: handle nullable metadata in OffsetFetchResponse by @dnwe in https://github.com/IBM/sarama/pull/3473
* fix: nil response/done channels after SASLv1 failure by @dnwe in https://github.com/IBM/sarama/pull/3474
* fix(protocol): handle ElectLeaders V1 non-flexible headers by @DCjanus in https://github.com/IBM/sarama/pull/3478
* fix: correct a number of goroutine leaks by @dnwe in https://github.com/IBM/sarama/pull/3476
* fix: resolve deadlock in concurrent offset commits by @dnwe in https://github.com/IBM/sarama/pull/3477
* fix(consumer): avoid broker race in response feeder by @DCjanus in https://github.com/IBM/sarama/pull/3486
* fix: stop dispatcher for dying children in brokerConsumer.abort() by @lizthegrey in https://github.com/IBM/sarama/pull/3492
* fix: close broken tcp connections by @Asphaltt in https://github.com/IBM/sarama/pull/3384
* fix: add Unwrap() to DescribeConfigError and AlterConfigError by @ShinThirty in https://github.com/IBM/sarama/pull/3487
### :package: Dependency updates
* chore(deps): update dependency golangci/golangci-lint to v2.11.1 by @renovate[bot] in https://github.com/IBM/sarama/pull/3462
* chore(deps): bump github.com/pierrec/lz4/v4 from 4.1.25 to 4.1.26 by @dependabot[bot] in https://github.com/IBM/sarama/pull/3461
* chore(deps): bump golang.org/x/sync from 0.19.0 to 0.20.0 in the golang-x group across 1 directory by @dependabot[bot] in https://github.com/IBM/sarama/pull/3466
* chore(deps): update module golang.org/x/crypto to v0.49.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3468
* chore(deps): update dependency golangci/golangci-lint to v2.11.3 by @renovate[bot] in https://github.com/IBM/sarama/pull/3464
* fix(deps): update module golang.org/x/net to v0.52.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3472
* fix(deps): update module github.com/klauspost/compress to v1.18.5 by @renovate[bot] in https://github.com/IBM/sarama/pull/3480
* chore(deps): update module golang.org/x/crypto to v0.50.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3489
* fix(deps): update module golang.org/x/net to v0.53.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3493
* chore(deps): update docker/setup-buildx-action action to v4 by @renovate[bot] in https://github.com/IBM/sarama/pull/3458
* chore(deps): update docker/bake-action action to v7.1.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3459
### :wrench: Maintenance
* chore: add kafka versions 3.9.2 and 4.2.0 by @edoardocomar in https://github.com/IBM/sarama/pull/3471
### :memo: Documentation
* Update the Kakfa Protocol Specification Link by @MohishKhadse55 in https://github.com/IBM/sarama/pull/3463
### :heavy_plus_sign: Other Changes
* chore(deps): update dependency golangci/golangci-lint to v2.11.4 by @renovate[bot] in https://github.com/IBM/sarama/pull/3482
* fix: update API version URL as previous link was not working by @MohishKhadse55 in https://github.com/IBM/sarama/pull/3485

## New Contributors
* @MohishKhadse55 made their first contribution in https://github.com/IBM/sarama/pull/3463
* @Asphaltt made their first contribution in https://github.com/IBM/sarama/pull/3384
* @ShinThirty made their first contribution in https://github.com/IBM/sarama/pull/3487

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.47.0...v1.48.0

## Version 1.47.0 (2026-02-27)

## What's Changed
### :tada: New Features / Improvements
* perf(admin): modernize DescribeCluster RPC handling by @DCjanus in https://github.com/IBM/sarama/pull/3390
* test: expand Java interop tests to cover all compression codecs by @dnwe in https://github.com/IBM/sarama/pull/3423
### :bug: Fixes
* fix(client): add nilguards to updateBroker by @dnwe in https://github.com/IBM/sarama/pull/3393
* fix(broker): auto-close broken connections by @DCjanus in https://github.com/IBM/sarama/pull/3412
* fix: set version from IBM/sarama, not main app by @adamdecaf in https://github.com/IBM/sarama/pull/3415
### :wrench: Maintenance
* chore: finish up the move to atomic types by @puellanivis in https://github.com/IBM/sarama/pull/3399
* chore: tear down zk in functional tests by @edoardocomar in https://github.com/IBM/sarama/pull/3420
* chore: migrate from eapache/go-xerial-snappy to klauspost/compress/sn… by @edoardocomar in https://github.com/IBM/sarama/pull/3421
* fix(test): resolve FVT issues in Kafka v2.x interop tests by @edoardocomar in https://github.com/IBM/sarama/pull/3424
* feat: add kafka 4.1.1 constants and use in FVT by @dnwe in https://github.com/IBM/sarama/pull/3437
* chore: add Kafka 4.0.1 and replace 4.0.0 in FVT by @edoardocomar in https://github.com/IBM/sarama/pull/3439
* ci(lint): unblock Go 1.26 lint and handle gosec noise by @DCjanus in https://github.com/IBM/sarama/pull/3454
### :package: Dependency updates
* chore(deps): update module golang.org/x/crypto to v0.45.0 [security] → v0.48.0 by @renovate[bot] in #3383, #3425
* chore(deps): bump golang.org/x/crypto from 0.42.0 to 0.45.0 across /examples (consumergroup, exactly_once, sasl_scram_client, http_server, txn_producer, interceptors) by @dependabot[bot] in #3382, #3381, #3380, #3379, #3378, #3377
* fix(deps): update module golang.org/x/sync to v0.18.0 by @renovate[bot] in #3385
* fix(deps): update module golang.org/x/net to v0.49.0 → v0.51.0 by @renovate[bot] and @dependabot[bot] in #3426, #3427, #3453
* chore(deps): update golangci/golangci-lint-action action to v9 → v9.2.0 by @renovate[bot] in #3370, #3400
* chore(deps): update dependency golangci/golangci-lint to v2.6.2 → v2.8.0 by @renovate[bot] in #3366, #3401
* chore(deps): update docker/bake-action action to v6.10.0 by @renovate[bot] in #3392
* chore(deps): update docker/setup-buildx-action action to v3.12.0 by @renovate[bot] in #3416
* chore(deps): bump github.com/klauspost/compress from 1.18.1 to 1.18.4 by @dependabot[bot] in #3397, #3430, #3442
* chore(deps): bump github.com/pierrec/lz4/v4 from 4.1.22 to 4.1.25 by @dependabot[bot] in #3411, #3432
* chore(deps): bump the golang-x group across 1 directory with 2 updates by @dependabot[bot] in #3405
* chore(deps): bump github.com/xdg-go/scram from 1.1.2 to 1.2.0 in /examples/sasl_scram_client by @dependabot[bot] in #3394
* chore(deps): update dependency dominikh/go-tools to v2026 by @renovate[bot] in #3446

## New Contributors
* @DCjanus made their first contribution in https://github.com/IBM/sarama/pull/3390
* @edoardocomar made their first contribution in https://github.com/IBM/sarama/pull/3420
* @adamdecaf made their first contribution in https://github.com/IBM/sarama/pull/3415

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.46.3...v1.47.0

## Version 1.46.3 (2025-10-26)

## What's Changed
### :bug: Fixes
* fix: wrap KError into error returned by IncrementalAlterConfig by @prestona in https://github.com/IBM/sarama/pull/3352
* fix: assign sequence when flushing retry buffers by @dnwe in https://github.com/IBM/sarama/pull/3362
### :package: Dependency updates
* chore(deps): update dependency dominikh/go-tools to v2025 by @renovate[bot] in https://github.com/IBM/sarama/pull/3351
* chore(deps): update dependency vearutop/teststat to v0.1.27 by @renovate[bot] in https://github.com/IBM/sarama/pull/3350
* fix(deps): update module github.com/klauspost/compress to v1.18.1 by @renovate[bot] in https://github.com/IBM/sarama/pull/3355
### :wrench: Maintenance
* chore(ci): extract tool versions and add renovate customManagers by @dnwe in https://github.com/IBM/sarama/pull/3346


**Full Changelog**: https://github.com/IBM/sarama/compare/v1.46.2...v1.46.3

## Version 1.46.2 (2025-10-10)

## What's Changed

A big focus on improving our support for newer protocol versions in this release, particularly supporting a wider range of flexible versions

### :tada: New Features / Improvements
* chore: support V5 ListOffsets by @dnwe in https://github.com/IBM/sarama/pull/3308
* feat: support DeleteGroups V2 protocol by @hindessm in https://github.com/IBM/sarama/pull/3320
* feat: support DeleteTopics V4 protocol by @hindessm in https://github.com/IBM/sarama/pull/3321
* feat: support CreateTopics V5 protocol by @hindessm in https://github.com/IBM/sarama/pull/3322
* feat: support IncrementalAlterConfigs V1 protocol by @hindessm in https://github.com/IBM/sarama/pull/3319
* feat: support DescribeGroups V5 protocol by @hindessm in https://github.com/IBM/sarama/pull/3331
* feat: support SyncGroup V4 protocol by @hindessm in https://github.com/IBM/sarama/pull/3332
* feat: support LeaveGroup V4 protocol by @hindessm in https://github.com/IBM/sarama/pull/3334
* feat: support Heartbeat V4 protocol by @hindessm in https://github.com/IBM/sarama/pull/3335
* feat: support JoinGroup V6 protocol by @hindessm in https://github.com/IBM/sarama/pull/3339
* feat: support DescribeClientQuotas V1 protocol by @dnwe in https://github.com/IBM/sarama/pull/3342
### :bug: Fixes
* fix: update map rather than create a new map by @hindessm in https://github.com/IBM/sarama/pull/3302
* fix: metadata_response valid version range by @hindessm in https://github.com/IBM/sarama/pull/3304
* fix: add V4 as valid CreateTopicsResponse by @dnwe in https://github.com/IBM/sarama/pull/3305
* fix: correct requiredVersion for DescribeLogDirsResponse by @dnwe in https://github.com/IBM/sarama/pull/3306
* fix: extend TestAllocateBodyProtocolVersions for more testing by @dnwe in https://github.com/IBM/sarama/pull/3307
* fix: non-flexible ElectLeadersRequest V0/V1 encode/decode by @hindessm in https://github.com/IBM/sarama/pull/3312
* fix: make alterPartitionReassignmentsBlock consistent by @hindessm in https://github.com/IBM/sarama/pull/3313
* fix: correct decodeRequest bytesRead return value by @hindessm in https://github.com/IBM/sarama/pull/3314
* fix: decoder issues by @hindessm in https://github.com/IBM/sarama/pull/3327
* fix: improve KIP-511 behaviour on older Kafka clusters by @dnwe in https://github.com/IBM/sarama/pull/3328
* fix: return correct error when encoding by @hindessm in https://github.com/IBM/sarama/pull/3333
* fix: correct ApiVersionsResponse handling of ErrUnsupportedVersion by @dnwe in https://github.com/IBM/sarama/pull/3337
### :package: Dependency updates
* chore(deps): update ossf/scorecard-action action to v2.4.3 by @renovate[bot] in https://github.com/IBM/sarama/pull/3318
* fix(deps): update module golang.org/x/net to v0.46.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3343
### :wrench: Maintenance
* chore: remove redundant insufficient data checks by @hindessm in https://github.com/IBM/sarama/pull/3300
* refactor: use struct rather than map with one entry by @hindessm in https://github.com/IBM/sarama/pull/3301
* chore(ci): adopt gotestsum and re-run flakes by @dnwe in https://github.com/IBM/sarama/pull/3311
* refactor: Flexible encoding/decoding refactoring by @hindessm in https://github.com/IBM/sarama/pull/3317
* chore(fvt): refactor docker-compose and support KRaft by @dnwe in https://github.com/IBM/sarama/pull/3323
* fix(fvt): simplify retry using testify's EventuallyWithT by @dnwe in https://github.com/IBM/sarama/pull/3324
* chore: add 3.9.1 and 4.1.0 version constants and FVT by @dnwe in https://github.com/IBM/sarama/pull/3325
* refactor: get/put for KError by @hindessm in https://github.com/IBM/sarama/pull/3326
* refactor: get/put for throttle time ms time.Duration by @hindessm in https://github.com/IBM/sarama/pull/3330
* chore(fvt): improve testFuncConsumerGroupMember by @dnwe in https://github.com/IBM/sarama/pull/3329
### :heavy_plus_sign: Other Changes
* fix(fvt): check err before usage by @dnwe in https://github.com/IBM/sarama/pull/3338


**Full Changelog**: https://github.com/IBM/sarama/compare/v1.46.1...v1.46.2

## Version 1.46.1 (2025-09-18)

> [!NOTE]
> The go.mod directive has been bumped to 1.24.0 as the minimum version of Go required for the module. This was necessary to continue to receive updates from some of the third party dependencies that Sarama makes use of.


## What's Changed
### :tada: New Features / Improvements
* feat: support more describe log dirs versions (V2-V4) by @hindessm in https://github.com/IBM/sarama/pull/3293
* feat: support V5 ListConsumerGroups protocol by @hindessm in https://github.com/IBM/sarama/pull/3292
* feat: add SASLv1 support for Kerberos by @dnwe in https://github.com/IBM/sarama/pull/3279
### :bug: Fixes
* fix: add read deadline to tls write by @bvalente in https://github.com/IBM/sarama/pull/3283
### :package: Dependency updates
* chore(deps): bump go directive to 1.24.0 and golang.org/x/{crypto,net,sync} by @dependabot[bot] in https://github.com/IBM/sarama/pull/3288
* chore(deps): bump the golang-x group across 6 directories with 1 update by @dependabot[bot] in https://github.com/IBM/sarama/pull/3291
* chore(deps): bump github.com/stretchr/testify from 1.11.0 to 1.11.1 by @dependabot[bot] in https://github.com/IBM/sarama/pull/3274
### :wrench: Maintenance
* chore: refactor to use modern atomic types by @Sahil-4555 in https://github.com/IBM/sarama/pull/3277
* chore: pre-commit autoupdate to latest by @dnwe in https://github.com/IBM/sarama/pull/3278
* chore: apply modernize fixes from gopls by @dnwe in https://github.com/IBM/sarama/pull/3297
* chore(config): update comments of sarama.Config.Metadata.SingleFlight by @gunli in https://github.com/IBM/sarama/pull/3296
* chore(client): update comments of client methods by @gunli in https://github.com/IBM/sarama/pull/3295

## New Contributors
* @Sahil-4555 made their first contribution in https://github.com/IBM/sarama/pull/3277
* @bvalente made their first contribution in https://github.com/IBM/sarama/pull/3283
* @gunli made their first contribution in https://github.com/IBM/sarama/pull/3296

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.46.0...v1.46.1

## Version 1.46.0 (2025-08-25)

> [!NOTE]
> This release contains significant changes. Notably Sarama will now use the ApiVersionRequest response from each broker to aid in selecting the protocol version to use. The existing `Version` field in sarama.Config will continue to provide a "pinning" mechanism, but can safely be set to a maximum or higher value than the remote cluster and sarama will sensibly pick compatible versions. There is also a performance improvement relating to MetadataRequests whereby Sarama will avoid having more than a single request to each broker in-flight at any given time. These new (optimal) behaviour is on by default can be opt-ed out via the `Metadata.SingleFlight` field in Config.

## What's Changed
### :tada: New Features / Improvements
* feat(protocol): negotiate API versions by @trapped in https://github.com/IBM/sarama/pull/3209
* feat: option to group metadata refreshes so only one is in-flight at a time by @cupcicm in https://github.com/IBM/sarama/pull/3225
* feat: use singleflight metadata by default by @dnwe in https://github.com/IBM/sarama/pull/3231
* feat(protocol): support CreateTopicRequest V4 by @dnwe in https://github.com/IBM/sarama/pull/3238
* feat: always send ApiVersionsRequest and fallback to v0 by @dnwe in https://github.com/IBM/sarama/pull/3234
### :bug: Fixes
* fix(consumer): stuck on the batch with zero records length by @sterligov in https://github.com/IBM/sarama/pull/3221
* fix: sync response header version to clamped request header by @trapped in https://github.com/IBM/sarama/pull/3223
* fix(decoder): handle null arrays correctly by @dnwe in https://github.com/IBM/sarama/pull/3144
* fix: hardcode lz4 writer blocksize to 64kb by @dnwe in https://github.com/IBM/sarama/pull/3258
### :package: Dependency updates
* chore(deps): bump the golang-x group across 1 directory with 2 updates by @dependabot[bot] in https://github.com/IBM/sarama/pull/3185
* chore(deps): bump the golang-x group across 7 directories with 2 updates by @dependabot[bot] in https://github.com/IBM/sarama/pull/3219
* fix(deps): update module golang.org/x/net to v0.43.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3244
* chore(deps): bump the golang-x group across 6 directories with 1 update by @dependabot[bot] in https://github.com/IBM/sarama/pull/3262
* chore(deps): update github/codeql-action action to v3.29.9 by @renovate[bot] in https://github.com/IBM/sarama/pull/3242
* fix(deps): update github.com/rcrowley/go-metrics digest to 65e299d by @renovate[bot] in https://github.com/IBM/sarama/pull/3164
* fix(deps): update module github.com/stretchr/testify to v1.11.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3268
* chore(deps): update docker/bake-action action to v6.9.0 by @renovate[bot] in https://github.com/IBM/sarama/pull/3264
### :wrench: Maintenance
* chore(lint): enable copyloopvar by @alexandear in https://github.com/IBM/sarama/pull/3214
* chore: fix inconsistent function name in comment by @stellrust in https://github.com/IBM/sarama/pull/3227
* chore(style): refactor compress.go for readability by @dnwe in https://github.com/IBM/sarama/pull/3260
* chore: replace unnecessary go-multierror dependency by @bestbug456 in https://github.com/IBM/sarama/pull/3243

## New Contributors
* @ibm-mend-app[bot] made their first contribution in https://github.com/IBM/sarama/pull/3201
* @alexandear made their first contribution in https://github.com/IBM/sarama/pull/3214
* @trapped made their first contribution in https://github.com/IBM/sarama/pull/3209
* @cupcicm made their first contribution in https://github.com/IBM/sarama/pull/3225
* @sterligov made their first contribution in https://github.com/IBM/sarama/pull/3221
* @stellrust made their first contribution in https://github.com/IBM/sarama/pull/3227
* @bestbug456 made their first contribution in https://github.com/IBM/sarama/pull/3243

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.45.2...v1.46.0

## Version 1.45.2 (2025-05-28)

## What's Changed
### :bug: Fixes
* fix(decoder): use configurable limit for max number of records in a record batch by @rmb938 in https://github.com/IBM/sarama/pull/3120
* fix: ensure mock SyncProducer's SendMessage returns msg.Partition instead of 0 by @magiusdarrigo in https://github.com/IBM/sarama/pull/3122
* fix: send null instead of empty string when describing default client quotas by @petedannemann in https://github.com/IBM/sarama/pull/3128
* fix: improve getMetricName performance by @boekkooi-impossiblecloud in https://github.com/IBM/sarama/pull/3156
### :package: Dependency updates
* chore(deps): bump github.com/klauspost/compress from 1.17.11 to 1.18.0 by @dependabot in https://github.com/IBM/sarama/pull/3103
* chore(deps): bump the golang-x group across 6 directories with 1 update by @dependabot in https://github.com/IBM/sarama/pull/3114
* chore(deps): bump the golang-x group across 7 directories with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/3121
* chore(deps): bump the go_modules group across 7 directories with 1 update by @dependabot in https://github.com/IBM/sarama/pull/3148
* chore(deps): bump the go_modules group across 7 directories with 1 update by @dependabot in https://github.com/IBM/sarama/pull/3157
* chore(deps): bump golang.org/x/sync from 0.12.0 to 0.14.0 in the golang-x group across 1 directory by @dependabot in https://github.com/IBM/sarama/pull/3161
### :heavy_plus_sign: Other Changes
* chore: bump minimum Go version to 1.23.0 by @dnwe in https://github.com/IBM/sarama/pull/3113
* fix(ci): bump golangci-lint to v2 by @dnwe in https://github.com/IBM/sarama/pull/3160

## New Contributors
* @rmb938 made their first contribution in https://github.com/IBM/sarama/pull/3120
* @magiusdarrigo made their first contribution in https://github.com/IBM/sarama/pull/3122
* @petedannemann made their first contribution in https://github.com/IBM/sarama/pull/3128
* @renovate made their first contribution in https://github.com/IBM/sarama/pull/3155
* @boekkooi-impossiblecloud made their first contribution in https://github.com/IBM/sarama/pull/3156

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.45.1...v1.45.2

## Version 1.45.1 (2025-03-02)

## What's Changed
### :tada: New Features / Improvements
* feat(producer): add MaxBufferBytes to limit retry buffer size by @wanwenli in https://github.com/IBM/sarama/pull/3088
* feat(producer): add sync pool for channel reuse by @kasimtj in https://github.com/IBM/sarama/pull/3109
* feat: exponential backoff for clients (KIP-580) by @wanwenli in https://github.com/IBM/sarama/pull/3099
### :bug: Fixes
* fix(sasl): add nilguard around token to prevent panic by @hoo47 in https://github.com/IBM/sarama/pull/3076
* fix(test): consumer group fetch request messages by @stsmurf in https://github.com/IBM/sarama/pull/3081
* fix: remove redundant nil check by @knbr13 in https://github.com/IBM/sarama/pull/3089
* fix(consumer): add recovery from no leader partitions by @liutao365 in https://github.com/IBM/sarama/pull/3101
* produce: set MaxTimestamp by @rockwotj in https://github.com/IBM/sarama/pull/3108
### :package: Dependency updates
* chore(deps): bump go.opentelemetry.io/otel from 1.24.0 to 1.29.0 in /examples/interceptors by @dependabot in https://github.com/IBM/sarama/pull/3071
* chore(deps): bump the otel group across 1 directory with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/3072
* chore(deps): bump the golang-x group across 1 directory with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/3098
### :wrench: Maintenance
* chore(deps): prevent otel upgrades for now by @dnwe in https://github.com/IBM/sarama/pull/3069
* chore: add version constant for kafka 3.7.2 by @dnwe in https://github.com/IBM/sarama/pull/3073
* chore(ci): fetch kafka 4.0 via tar.gz rather than git by @dnwe in https://github.com/IBM/sarama/pull/3079
* fix(ci): tighten up github workflows by @dnwe in https://github.com/IBM/sarama/pull/3080
* chore(ci): analyse actions in codeql by @dnwe in https://github.com/IBM/sarama/pull/3085
* chore(ci): bump golangci-lint version to v1.63.4 by @dnwe in https://github.com/IBM/sarama/pull/3090
* feat(ci): add dedicated staticcheck run by @dnwe in https://github.com/IBM/sarama/pull/3091

## New Contributors
* @hoo47 made their first contribution in https://github.com/IBM/sarama/pull/3076
* @stsmurf made their first contribution in https://github.com/IBM/sarama/pull/3081
* @knbr13 made their first contribution in https://github.com/IBM/sarama/pull/3089
* @liutao365 made their first contribution in https://github.com/IBM/sarama/pull/3101
* @rockwotj made their first contribution in https://github.com/IBM/sarama/pull/3108
* @kasimtj made their first contribution in https://github.com/IBM/sarama/pull/3109

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.45.0...v1.45.1

## Version 1.45.0 (2025-01-07)

> [!NOTE]
> The go.mod directive has been bumped to 1.21 as the minimum version of Go required for the module. This was necessary to continue to receive updates from some of the third party dependencies that Sarama makes use of for compression.

## What's Changed
### :bug: Fixes
* fix(admin): add retries for GroupCoordinator errors by @dnwe in https://github.com/IBM/sarama/pull/3053
### :package: Dependency updates
* chore(deps): bump github.com/klauspost/compress from 1.17.9 to 1.17.11 by @dependabot in https://github.com/IBM/sarama/pull/2999
* chore(deps): bump golang.org/x/net from 0.33.0 to 0.34.0 in the golang-org-x group by @dependabot in https://github.com/IBM/sarama/pull/3054
### :wrench: Maintenance
* chore: bump minimum go to 1.21 by @dnwe in https://github.com/IBM/sarama/pull/3048
* chore(test): tag all unittests as !integration by @dnwe in https://github.com/IBM/sarama/pull/3047
* chore(test): include kafka 4.0.0 in FV testing by @dnwe in https://github.com/IBM/sarama/pull/3045
* fix(ci): restore the Kafka 4.0.0 FV by @dnwe in https://github.com/IBM/sarama/pull/3055


**Full Changelog**: https://github.com/IBM/sarama/compare/v1.44.0...v1.45.0

## Version 1.44.0 (2024-12-27)

> [!NOTE]
> The go.mod directive has been bumped to 1.20 as the minimum version of Go required for the module. This was necessary to continue to receive updates from some of the third party dependencies that Sarama makes use of for compression.

## What's Changed
### :tada: New Features / Improvements
* feat: update go directive to 1.20 by @mauri870 in https://github.com/IBM/sarama/pull/2933
* feat(producer): add retry buffer tuning option to prevent OOM by @wanwenli in https://github.com/IBM/sarama/pull/3026
* feat(admin): implement leader election api by @chengjoey in https://github.com/IBM/sarama/pull/3030
### :bug: Fixes
* fix: log SASL connection and handshake errors by @pierDipi in https://github.com/IBM/sarama/pull/2995
### :package: Dependency updates
* chore(deps): bump the golang-org-x group across 1 directory with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/3010
* chore(deps): bump golang.org/x/crypto from 0.28.0 to 0.31.0 in the go_modules group by @dependabot in https://github.com/IBM/sarama/pull/3041
* chore(deps): bump the golang-org-x group across 1 directory with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/3040
* chore(deps): bump github.com/pierrec/lz4/v4 from 4.1.21 to 4.1.22 by @dependabot in https://github.com/IBM/sarama/pull/3038
* chore(deps): bump the go_modules group across 2 directories with 1 update by @dependabot in https://github.com/IBM/sarama/pull/3035
* chore(deps): bump golang.org/x/crypto from 0.22.0 to 0.31.0 in /examples/consumergroup in the go_modules group by @dependabot in https://github.com/IBM/sarama/pull/3033
* chore(deps): bump golang.org/x/crypto from 0.22.0 to 0.31.0 in /examples/txn_producer in the go_modules group by @dependabot in https://github.com/IBM/sarama/pull/3034
* chore(deps): bump golang.org/x/crypto from 0.22.0 to 0.31.0 in /examples/interceptors in the go_modules group by @dependabot in https://github.com/IBM/sarama/pull/3032
* chore(deps): bump golang.org/x/crypto from 0.22.0 to 0.31.0 in /examples/exactly_once in the go_modules group by @dependabot in https://github.com/IBM/sarama/pull/3031
* chore(deps): bump github.com/stretchr/testify from 1.9.0 to 1.10.0 by @dependabot in https://github.com/IBM/sarama/pull/3020
### :wrench: Maintenance
* chore: add newer kafka versions and bump Go in CI by @dnwe in https://github.com/IBM/sarama/pull/2969
* fix(lint): resolve IDENTICAL_BRANCHES issue in broker by @frzifus in https://github.com/IBM/sarama/pull/2992
* chore: add version consts for 3.8.1+3.9.0 by @dnwe in https://github.com/IBM/sarama/pull/3011
* fix(client): refactor duplicated replica+partition logic by @Trinoooo in https://github.com/IBM/sarama/pull/2925
* chore(deps): bump golang.org/x/net to v0.33.0 by @dnwe in https://github.com/IBM/sarama/pull/3044

## New Contributors
* @mauri870 made their first contribution in https://github.com/IBM/sarama/pull/2933
* @frzifus made their first contribution in https://github.com/IBM/sarama/pull/2992
* @pierDipi made their first contribution in https://github.com/IBM/sarama/pull/2995
* @wanwenli made their first contribution in https://github.com/IBM/sarama/pull/3026
* @Trinoooo made their first contribution in https://github.com/IBM/sarama/pull/2925
* @chengjoey made their first contribution in https://github.com/IBM/sarama/pull/3030

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.43.3...v1.44.0

## Version 1.43.3 (2024-08-12)

## What's Changed
### :bug: Fixes
* fix: declare assignor variable for examples & clean up log format by @kumakichi in https://github.com/IBM/sarama/pull/2909
* fix(consumer): maintain ordering of offset commit requests by @prestona in https://github.com/IBM/sarama/pull/2947
* fix(producer): treat ErrKafkaStorageError as retriable by @richardartoul in https://github.com/IBM/sarama/pull/2939
### :package: Dependency updates
* chore(deps): bump the golang-org-x group across 1 directory with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/2956
* chore(deps): bump github.com/eapache/go-resiliency from 1.6.0 to 1.7.0 by @dependabot in https://github.com/IBM/sarama/pull/2944
* chore(deps): bump github.com/klauspost/compress from 1.17.8 to 1.17.9 by @dependabot in https://github.com/IBM/sarama/pull/2926
### :wrench: Maintenance
* fix(ci): correct docker-compose install by @dnwe in https://github.com/IBM/sarama/pull/2954
### :memo: Documentation
* fix(doc): correct JVM's config name corresponding to MaxWaitTime by @abhipranay in https://github.com/IBM/sarama/pull/2893

## New Contributors
* @abhipranay made their first contribution in https://github.com/IBM/sarama/pull/2893
* @kumakichi made their first contribution in https://github.com/IBM/sarama/pull/2909
* @richardartoul made their first contribution in https://github.com/IBM/sarama/pull/2939

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.43.2...v1.43.3

## Version 1.43.2 (2024-04-25)

## What's Changed
### :bug: Fixes
* chore(ci): add 32-bit alignment check by @dnwe in https://github.com/IBM/sarama/pull/2874
### :package: Dependency updates
* chore(deps): bump golang.org/x/net from 0.21.0 to 0.23.0 by @dependabot in https://github.com/IBM/sarama/pull/2866
* chore(deps): bump the golang-org-x group with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/2853
* chore(deps): bump github.com/klauspost/compress from 1.17.7 to 1.17.8 by @dependabot in https://github.com/IBM/sarama/pull/2857
* chore(deps): bump golang.org/x/net from 0.21.0 to 0.23.0 in /examples/txn_producer in the go_modules group by @dependabot in https://github.com/IBM/sarama/pull/2865
* chore(deps): bump golang.org/x/net from 0.21.0 to 0.23.0 in /examples/consumergroup in the go_modules group by @dependabot in https://github.com/IBM/sarama/pull/2867
* chore(deps): bump golang.org/x/net from 0.21.0 to 0.23.0 in /examples/exactly_once in the go_modules group by @dependabot in https://github.com/IBM/sarama/pull/2868
* chore(deps): bump golang.org/x/net from 0.22.0 to 0.23.0 in /examples/interceptors in the go_modules group by @dependabot in https://github.com/IBM/sarama/pull/2869


**Full Changelog**: https://github.com/IBM/sarama/compare/v1.43.1...v1.43.2

## Version 1.43.1 (2024-03-27)

## What's Changed
### :bug: Fixes
* fix: message.max.bytes should default to 1048576 not 1 MB by @puellanivis in https://github.com/IBM/sarama/pull/2804
* fix: add locking around broker throttle timer to prevent race condition by @chengsha in https://github.com/IBM/sarama/pull/2826
### :package: Dependency updates
* chore(deps): bump go.opentelemetry.io/otel/sdk from 1.23.1 to 1.24.0 in /examples/interceptors by @dependabot in https://github.com/IBM/sarama/pull/2816
* chore(deps): bump the golang-org-x group with 1 update by @dependabot in https://github.com/IBM/sarama/pull/2825
* chore(deps): bump github.com/stretchr/testify from 1.8.4 to 1.9.0 by @dependabot in https://github.com/IBM/sarama/pull/2822
* chore(deps): bump go.opentelemetry.io/otel/exporters/stdout/stdoutmetric from 1.23.1 to 1.24.0 in /examples/interceptors by @dependabot in https://github.com/IBM/sarama/pull/2815

## New Contributors
* @chengsha made their first contribution in https://github.com/IBM/sarama/pull/2826

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.43.0...v1.43.1

## Version 1.43.0 (2024-02-22)

> [!NOTE]
> The go.mod directive has been bumped to 1.19 as the minimum version of Go required for the module. This was necessary to continue to receive updates from some of the third party dependencies that Sarama makes use of for compression.

## What's Changed
### :tada: New Features / Improvements
* feat: update go directive to 1.19 by @dnwe in https://github.com/IBM/sarama/pull/2795
* feat: add BuildSpnFunc to GSSAPIConfig for allow custom spn by @fooofei in https://github.com/IBM/sarama/pull/2807
### :bug: Fixes
* Use %v formatting words and remove unnecessary newline by @puellanivis in https://github.com/IBM/sarama/pull/2802
### :package: Dependency updates
* chore(deps): bump github.com/klauspost/compress from 1.16.7 to 1.17.6 by @dependabot in https://github.com/IBM/sarama/pull/2784
* chore(deps): bump github.com/eapache/go-resiliency from 1.5.0 to 1.6.0 by @dependabot in https://github.com/IBM/sarama/pull/2810
* chore(deps): bump github.com/klauspost/compress from 1.17.6 to 1.17.7 by @dependabot in https://github.com/IBM/sarama/pull/2811
### :wrench: Maintenance
* chore(doc): add v1.42.2 to CHANGELOG.md by @dnwe in https://github.com/IBM/sarama/pull/2796

## New Contributors
* @puellanivis made their first contribution in https://github.com/IBM/sarama/pull/2802
* @fooofei made their first contribution in https://github.com/IBM/sarama/pull/2807

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.42.2...v1.43.0

## Version 1.42.2 (2024-02-09)

## What's Changed

⚠️ The go.mod directive has been bumped to 1.18 as the minimum version of Go required for the module. This was necessary to continue to receive updates from some of the third party dependencies that Sarama makes use of for compression.

### :tada: New Features / Improvements
* feat: update go directive to 1.18 by @dnwe in https://github.com/IBM/sarama/pull/2713
* feat: return KError instead of errors in AlterConfigs and DescribeConfig by @zhuliquan in https://github.com/IBM/sarama/pull/2472
### :bug: Fixes
* fix: don't waste time for backoff on member id required error by @lzakharov in https://github.com/IBM/sarama/pull/2759
* fix: prevent ConsumerGroup.Close infinitely locking by @maqdev in https://github.com/IBM/sarama/pull/2717
### :package: Dependency updates
* chore(deps): bump golang.org/x/net from 0.17.0 to 0.18.0 by @dependabot in https://github.com/IBM/sarama/pull/2716
* chore(deps): bump golang.org/x/sync to v0.5.0 by @dependabot in https://github.com/IBM/sarama/pull/2718
* chore(deps): bump github.com/pierrec/lz4/v4 from 4.1.18 to 4.1.19 by @dependabot in https://github.com/IBM/sarama/pull/2739
* chore(deps): bump golang.org/x/crypto from 0.15.0 to 0.17.0 by @dependabot in https://github.com/IBM/sarama/pull/2748
* chore(deps): bump the golang-org-x group with 1 update by @dependabot in https://github.com/IBM/sarama/pull/2734
* chore(deps): bump the golang-org-x group with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/2764
* chore(deps): bump github.com/pierrec/lz4/v4 from 4.1.19 to 4.1.21 by @dependabot in https://github.com/IBM/sarama/pull/2763
* chore(deps): bump golang.org/x/crypto from 0.15.0 to 0.17.0 in /examples/exactly_once by @dependabot in https://github.com/IBM/sarama/pull/2749
* chore(deps): bump golang.org/x/crypto from 0.15.0 to 0.17.0 in /examples/consumergroup by @dependabot in https://github.com/IBM/sarama/pull/2750
* chore(deps): bump golang.org/x/crypto from 0.15.0 to 0.17.0 in /examples/sasl_scram_client by @dependabot in https://github.com/IBM/sarama/pull/2751
* chore(deps): bump golang.org/x/crypto from 0.15.0 to 0.17.0 in /examples/interceptors by @dependabot in https://github.com/IBM/sarama/pull/2752
* chore(deps): bump golang.org/x/crypto from 0.15.0 to 0.17.0 in /examples/http_server by @dependabot in https://github.com/IBM/sarama/pull/2753
* chore(deps): bump github.com/eapache/go-resiliency from 1.4.0 to 1.5.0 by @dependabot in https://github.com/IBM/sarama/pull/2745
* chore(deps): bump golang.org/x/crypto from 0.15.0 to 0.17.0 in /examples/txn_producer by @dependabot in https://github.com/IBM/sarama/pull/2754
* chore(deps): bump go.opentelemetry.io/otel/sdk from 1.19.0 to 1.22.0 in /examples/interceptors by @dependabot in https://github.com/IBM/sarama/pull/2767
* chore(deps): bump the golang-org-x group with 1 update by @dependabot in https://github.com/IBM/sarama/pull/2793
* chore(deps): bump go.opentelemetry.io/otel/exporters/stdout/stdoutmetric from 0.42.0 to 1.23.1 in /examples/interceptors by @dependabot in https://github.com/IBM/sarama/pull/2792
### :wrench: Maintenance
* fix(examples): housekeeping of code and deps by @dnwe in https://github.com/IBM/sarama/pull/2720
### :heavy_plus_sign: Other Changes
* fix(test): retry MockBroker Listen for EADDRINUSE by @dnwe in https://github.com/IBM/sarama/pull/2721

## New Contributors
* @maqdev made their first contribution in https://github.com/IBM/sarama/pull/2717
* @zhuliquan made their first contribution in https://github.com/IBM/sarama/pull/2472

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.42.1...v1.42.2

## Version 1.42.1 (2023-11-07)

## What's Changed
### :bug: Fixes
* fix: make fetchInitialOffset use correct protocol by @dnwe in https://github.com/IBM/sarama/pull/2705
* fix(config): relax ClientID validation after 1.0.0 by @dnwe in https://github.com/IBM/sarama/pull/2706

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.42.0...v1.42.1

## Version 1.42.0 (2023-11-02)

## What's Changed
### :bug: Fixes
* Asynchronously close brokers during a RefreshBrokers by @bmassemin in https://github.com/IBM/sarama/pull/2693
* Fix data race on Broker.done channel by @prestona in https://github.com/IBM/sarama/pull/2698
* fix: data race in Broker.AsyncProduce by @lzakharov in https://github.com/IBM/sarama/pull/2678
* Fix default retention time value in offset commit by @prestona in https://github.com/IBM/sarama/pull/2700
* fix(txmgr): ErrOffsetsLoadInProgress is retriable by @dnwe in https://github.com/IBM/sarama/pull/2701
### :wrench: Maintenance
* chore(ci): improve ossf scorecard result by @dnwe in https://github.com/IBM/sarama/pull/2685
* chore(ci): add kafka 3.6.0 to FVT and versions by @dnwe in https://github.com/IBM/sarama/pull/2692
### :heavy_plus_sign: Other Changes
* chore(ci): ossf scorecard.yml by @dnwe in https://github.com/IBM/sarama/pull/2683
* fix(ci): always run CodeQL on every commit by @dnwe in https://github.com/IBM/sarama/pull/2689
* chore(doc): add OpenSSF Scorecard badge by @dnwe in https://github.com/IBM/sarama/pull/2691

## New Contributors
* @bmassemin made their first contribution in https://github.com/IBM/sarama/pull/2693
* @lzakharov made their first contribution in https://github.com/IBM/sarama/pull/2678

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.41.3...v1.42.0

## Version 1.41.3 (2023-10-17)

## What's Changed
### :bug: Fixes
* fix: pre-compile regex for parsing kafka version by @qshuai in https://github.com/IBM/sarama/pull/2663
* fix(client): ignore empty Metadata responses when refreshing by @HaoSunUber in https://github.com/IBM/sarama/pull/2672
### :package: Dependency updates
* chore(deps): bump the golang-org-x group with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/2661
* chore(deps): bump golang.org/x/net from 0.16.0 to 0.17.0 by @dependabot in https://github.com/IBM/sarama/pull/2671
### :memo: Documentation
* fix(docs): correct topic name in rebalancing strategy example by @maksadbek in https://github.com/IBM/sarama/pull/2657

## New Contributors
* @maksadbek made their first contribution in https://github.com/IBM/sarama/pull/2657
* @qshuai made their first contribution in https://github.com/IBM/sarama/pull/2663

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.41.2...v1.41.3

## Version 1.41.2 (2023-09-12)

## What's Changed
### :tada: New Features / Improvements
* perf: Alloc records in batch by @ronanh in https://github.com/IBM/sarama/pull/2646
### :bug: Fixes
* fix(consumer): guard against nil client by @dnwe in https://github.com/IBM/sarama/pull/2636
* fix(consumer): don't retry session if ctx canceled by @dnwe in https://github.com/IBM/sarama/pull/2642
* fix: use least loaded broker to refresh metadata by @HaoSunUber in https://github.com/IBM/sarama/pull/2645
### :package: Dependency updates
* chore(deps): bump the golang-org-x group with 1 update by @dependabot in https://github.com/IBM/sarama/pull/2641

## New Contributors
* @HaoSunUber made their first contribution in https://github.com/IBM/sarama/pull/2645

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.41.1...v1.41.2

## Version 1.41.1 (2023-08-30)

## What's Changed
### :bug: Fixes
* fix(proto): handle V3 member metadata and empty owned partitions by @dnwe in https://github.com/IBM/sarama/pull/2618
* fix: make clear that error is configuration issue not server error by @hindessm in https://github.com/IBM/sarama/pull/2628
* fix(client): force Event Hubs to use V1_0_0_0 by @dnwe in https://github.com/IBM/sarama/pull/2633
* fix: add retries to alter user scram creds by @hindessm in https://github.com/IBM/sarama/pull/2632
### :wrench: Maintenance
* chore(lint): bump golangci-lint and tweak config by @dnwe in https://github.com/IBM/sarama/pull/2620
### :memo: Documentation
* fix(doc): add missing doc for mock consumer by @hsweif in https://github.com/IBM/sarama/pull/2386
* chore(proto): doc CreateTopics/JoinGroup fields by @dnwe in https://github.com/IBM/sarama/pull/2627
### :heavy_plus_sign: Other Changes
* chore(gh): add new style issue templates by @dnwe in https://github.com/IBM/sarama/pull/2624


**Full Changelog**: https://github.com/IBM/sarama/compare/v1.41.0...v1.41.1

## Version 1.41.0 (2023-08-21)

## What's Changed
### :rotating_light: Breaking Changes

Note: this version of Sarama has had a big overhaul in its adherence to the use of the right Kafka protocol versions for the given Config Version. It has also bumped the default Version set in Config (where one is not supplied) to 2.1.0. This is in preparation for Kafka 4.0 dropping support for protocol versions older than 2.1. If you are using Sarama against Kafka clusters older than v2.1.0, or using it against Azure EventHubs then you will likely have to change your application code to pin to the appropriate Version.

* chore(config): make DefaultVersion V2_0_0_0 by @dnwe in https://github.com/IBM/sarama/pull/2572
* chore(config): make DefaultVersion V2_1_0_0 by @dnwe in https://github.com/IBM/sarama/pull/2574
### :tada: New Features / Improvements
* Implement resolve_canonical_bootstrap_servers_only by @gebn in https://github.com/IBM/sarama/pull/2156
* feat: sleep when throttled (KIP-219) by @hindessm in https://github.com/IBM/sarama/pull/2536
* feat: add isValidVersion to protocol types by @dnwe in https://github.com/IBM/sarama/pull/2538
* fix(consumer): use newer LeaveGroup as appropriate by @dnwe in https://github.com/IBM/sarama/pull/2544
* Add support for up to version 4 List Groups API by @prestona in https://github.com/IBM/sarama/pull/2541
* fix(producer): use newer ProduceReq as appropriate by @dnwe in https://github.com/IBM/sarama/pull/2546
* fix(proto): ensure req+resp requiredVersion match by @dnwe in https://github.com/IBM/sarama/pull/2548
* chore(proto): permit CreatePartitionsRequest V1 by @dnwe in https://github.com/IBM/sarama/pull/2549
* chore(proto): permit AlterConfigsRequest V1 by @dnwe in https://github.com/IBM/sarama/pull/2550
* chore(proto): permit DeleteGroupsRequest V1 by @dnwe in https://github.com/IBM/sarama/pull/2551
* fix(proto): correct JoinGroup usage for wider version range by @dnwe in https://github.com/IBM/sarama/pull/2553
* fix(consumer): use full range of FetchRequest vers by @dnwe in https://github.com/IBM/sarama/pull/2554
* fix(proto): use range of OffsetCommitRequest vers by @dnwe in https://github.com/IBM/sarama/pull/2555
* fix(proto): use full range of MetadataRequest by @dnwe in https://github.com/IBM/sarama/pull/2556
* fix(proto): use fuller ranges of supported proto by @dnwe in https://github.com/IBM/sarama/pull/2558
* fix(proto): use full range of SyncGroupRequest by @dnwe in https://github.com/IBM/sarama/pull/2565
* fix(proto): use full range of ListGroupsRequest by @dnwe in https://github.com/IBM/sarama/pull/2568
* feat(proto): support for Metadata V6-V10 by @dnwe in https://github.com/IBM/sarama/pull/2566
* fix(proto): use full ranges for remaining proto by @dnwe in https://github.com/IBM/sarama/pull/2570
* feat(proto): add remaining protocol for V2.1 by @dnwe in https://github.com/IBM/sarama/pull/2573
* feat: add new error for MockDeleteTopicsResponse by @javiercri in https://github.com/IBM/sarama/pull/2475
* feat(gzip): switch to klauspost/compress gzip by @dnwe in https://github.com/IBM/sarama/pull/2600
### :bug: Fixes
* fix: correct unsupported version check by @hindessm in https://github.com/IBM/sarama/pull/2528
* fix: avoiding burning cpu if all partitions are paused by @napallday in https://github.com/IBM/sarama/pull/2532
* extend throttling metric scope by @hindessm in https://github.com/IBM/sarama/pull/2533
* Fix printing of final metrics by @prestona in https://github.com/IBM/sarama/pull/2545
* fix(consumer): cannot automatically fetch newly-added partitions unless restart by @napallday in https://github.com/IBM/sarama/pull/2563
* bug: implement unsigned modulus for partitioning with crc32 hashing by @csm8118 in https://github.com/IBM/sarama/pull/2560
* fix: avoid logging value of proxy.Dialer by @prestona in https://github.com/IBM/sarama/pull/2569
* fix(test): add missing closes to admin client tests by @dnwe in https://github.com/IBM/sarama/pull/2594
* fix(test): ensure some more clients are closed by @dnwe in https://github.com/IBM/sarama/pull/2595
* fix(examples): sync exactly_once and consumergroup by @dnwe in https://github.com/IBM/sarama/pull/2614
* fix(fvt): fresh metrics registry for each test by @dnwe in https://github.com/IBM/sarama/pull/2616
* fix(test): flaky test TestFuncOffsetManager by @napallday in https://github.com/IBM/sarama/pull/2609
### :package: Dependency updates
* chore(deps): bump the golang-org-x group with 1 update by @dependabot in https://github.com/IBM/sarama/pull/2542
* chore(deps): bump the golang-org-x group with 1 update by @dependabot in https://github.com/IBM/sarama/pull/2561
* chore(deps): bump module github.com/pierrec/lz4/v4 to v4.1.18 by @dnwe in https://github.com/IBM/sarama/pull/2589
* chore(deps): bump module github.com/jcmturner/gokrb5/v8 to v8.4.4 by @dnwe in https://github.com/IBM/sarama/pull/2587
* chore(deps): bump github.com/eapache/go-xerial-snappy digest to c322873 by @dnwe in https://github.com/IBM/sarama/pull/2586
* chore(deps): bump module github.com/klauspost/compress to v1.16.7 by @dnwe in https://github.com/IBM/sarama/pull/2588
* chore(deps): bump github.com/eapache/go-resiliency from 1.3.0 to 1.4.0 by @dependabot in https://github.com/IBM/sarama/pull/2598
### :wrench: Maintenance
* fix(fvt): ensure fully-replicated at test start by @hindessm in https://github.com/IBM/sarama/pull/2531
* chore: rollup fvt kafka to latest three by @dnwe in https://github.com/IBM/sarama/pull/2537
* Merge the two CONTRIBUTING.md's by @prestona in https://github.com/IBM/sarama/pull/2543
* fix(test): test timing error by @hindessm in https://github.com/IBM/sarama/pull/2552
* chore(ci): tidyup and improve actions workflows by @dnwe in https://github.com/IBM/sarama/pull/2557
* fix(test): shutdown MockBroker by @dnwe in https://github.com/IBM/sarama/pull/2571
* chore(proto): match HeartbeatResponse version by @dnwe in https://github.com/IBM/sarama/pull/2576
* chore(test): ensure MockBroker closed within test by @dnwe in https://github.com/IBM/sarama/pull/2575
* chore(test): ensure all mockresponses use version by @dnwe in https://github.com/IBM/sarama/pull/2578
* chore(ci): use latest Go in actions by @dnwe in https://github.com/IBM/sarama/pull/2580
* chore(test): speedup some slow tests by @dnwe in https://github.com/IBM/sarama/pull/2579
* chore(test): use modern protocol versions in FVT by @dnwe in https://github.com/IBM/sarama/pull/2581
* chore(test): fix a couple of leaks by @dnwe in https://github.com/IBM/sarama/pull/2591
* feat(fvt): experiment with per-kafka-version image by @dnwe in https://github.com/IBM/sarama/pull/2592
* chore(ci): replace toxiproxy client dep by @dnwe in https://github.com/IBM/sarama/pull/2593
* feat(fvt): add healthcheck, depends_on and --wait by @dnwe in https://github.com/IBM/sarama/pull/2601
* fix(fvt): handle msgset vs batchset by @dnwe in https://github.com/IBM/sarama/pull/2603
* fix(fvt): Metadata version in ensureFullyReplicated by @dnwe in https://github.com/IBM/sarama/pull/2612
* fix(fvt): versioned cfg for invalid topic producer by @dnwe in https://github.com/IBM/sarama/pull/2613
* chore(fvt): tweak to work across more versions by @dnwe in https://github.com/IBM/sarama/pull/2615
* feat(fvt): test wider range of kafkas by @dnwe in https://github.com/IBM/sarama/pull/2605
### :memo: Documentation
* fix(example): check if msg channel is closed by @ioanzicu in https://github.com/IBM/sarama/pull/2479
* chore: use go install for installing sarama tools by @vigith in https://github.com/IBM/sarama/pull/2599

## New Contributors
* @gebn made their first contribution in https://github.com/IBM/sarama/pull/2156
* @prestona made their first contribution in https://github.com/IBM/sarama/pull/2543
* @ioanzicu made their first contribution in https://github.com/IBM/sarama/pull/2479
* @csm8118 made their first contribution in https://github.com/IBM/sarama/pull/2560
* @javiercri made their first contribution in https://github.com/IBM/sarama/pull/2475
* @vigith made their first contribution in https://github.com/IBM/sarama/pull/2599

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.40.1...v1.41.0

## Version 1.40.1 (2023-07-27)

## What's Changed
### :tada: New Features / Improvements
* Use buffer pools for decompression by @ronanh in https://github.com/IBM/sarama/pull/2484
* feat: support for Kerberos authentication with a credentials cache. by @mrogaski in https://github.com/IBM/sarama/pull/2457
### :bug: Fixes
* Fix some retry issues by @hindessm in https://github.com/IBM/sarama/pull/2517
* fix: admin retry logic by @hindessm in https://github.com/IBM/sarama/pull/2519
* Add some retry logic to more admin client functions by @hindessm in https://github.com/IBM/sarama/pull/2520
* fix: concurrent issue on updateMetadataMs by @napallday in https://github.com/IBM/sarama/pull/2522
* fix(test): allow testing of skipped test without IsTransactional panic by @hindessm in https://github.com/IBM/sarama/pull/2525
### :package: Dependency updates
* chore(deps): bump the golang-org-x group with 2 updates by @dependabot in https://github.com/IBM/sarama/pull/2509
* chore(deps): bump github.com/klauspost/compress from 1.15.14 to 1.16.6 by @dependabot in https://github.com/IBM/sarama/pull/2513
* chore(deps): bump github.com/stretchr/testify from 1.8.1 to 1.8.3 by @dependabot in https://github.com/IBM/sarama/pull/2512
### :wrench: Maintenance
* chore(ci): migrate probot-stale to actions/stale by @dnwe in https://github.com/IBM/sarama/pull/2496
* chore(ci): bump golangci version, cleanup, depguard config by @EladLeev in https://github.com/IBM/sarama/pull/2504
* Clean up some typos and docs/help mistakes by @hindessm in https://github.com/IBM/sarama/pull/2514
### :heavy_plus_sign: Other Changes
* chore(ci): add simple apidiff workflow by @dnwe in https://github.com/IBM/sarama/pull/2497
* chore(ci): bump actions/setup-go from 3 to 4 by @dependabot in https://github.com/IBM/sarama/pull/2508
* fix(comments): PauseAll and ResumeAll by @napallday in https://github.com/IBM/sarama/pull/2523

## New Contributors
* @EladLeev made their first contribution in https://github.com/IBM/sarama/pull/2504
* @hindessm made their first contribution in https://github.com/IBM/sarama/pull/2514
* @ronanh made their first contribution in https://github.com/IBM/sarama/pull/2484
* @mrogaski made their first contribution in https://github.com/IBM/sarama/pull/2457

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.40.0...v1.40.1

## Version 1.40.0 (2023-07-17)

## What's Changed

Note: this is the first release after the transition of Sarama ownership from Shopify to IBM in https://github.com/IBM/sarama/issues/2461

### :rotating_light: Breaking Changes

- chore: migrate module to github.com/IBM/sarama by @dnwe in https://github.com/IBM/sarama/pull/2492
- fix: restore (\*OffsetCommitRequest) AddBlock func by @dnwe in https://github.com/IBM/sarama/pull/2494

### :bug: Fixes

- fix(consumer): don't retry FindCoordinator forever by @dnwe in https://github.com/IBM/sarama/pull/2427
- fix(metrics): fix race condition when calling Broker.Open() twice by @vincentbernat in https://github.com/IBM/sarama/pull/2428
- fix: use version 4 of DescribeGroupsRequest only if kafka broker vers… …ion is >= 2.4 by @faillefer in https://github.com/IBM/sarama/pull/2451
- Fix HighWaterMarkOffset of mocks partition consumer by @gr8web in https://github.com/IBM/sarama/pull/2447
- fix: prevent data race in balance strategy by @napallday in https://github.com/IBM/sarama/pull/2453

### :package: Dependency updates

- chore(deps): bump golang.org/x/net from 0.5.0 to 0.7.0 by @dependabot in https://github.com/IBM/sarama/pull/2452

### :wrench: Maintenance

- chore: add kafka 3.3.2 by @dnwe in https://github.com/IBM/sarama/pull/2434
- chore(ci): remove Shopify/shopify-cla-action by @dnwe in https://github.com/IBM/sarama/pull/2489
- chore: bytes.Equal instead bytes.Compare by @testwill in https://github.com/IBM/sarama/pull/2485

## New Contributors

- @dependabot made their first contribution in https://github.com/IBM/sarama/pull/2452
- @gr8web made their first contribution in https://github.com/IBM/sarama/pull/2447
- @testwill made their first contribution in https://github.com/IBM/sarama/pull/2485

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.38.1...v1.40.0

## Version 1.38.1 (2023-01-22)

## What's Changed
### :bug: Fixes
* fix(example): correct `records-number` param in txn producer readme by @diallo-han in https://github.com/IBM/sarama/pull/2420
* fix: use newConsumer method in newConsumerGroup method by @Lumotheninja in https://github.com/IBM/sarama/pull/2424
### :package: Dependency updates
* chore(deps): bump module github.com/klauspost/compress to v1.15.14 by @dnwe in https://github.com/IBM/sarama/pull/2410
* chore(deps): bump module golang.org/x/net to v0.5.0 by @dnwe in https://github.com/IBM/sarama/pull/2413
* chore(deps): bump module github.com/stretchr/testify to v1.8.1 by @dnwe in https://github.com/IBM/sarama/pull/2411
* chore(deps): bump module github.com/xdg-go/scram to v1.1.2 by @dnwe in https://github.com/IBM/sarama/pull/2412
* chore(deps): bump module golang.org/x/sync to v0.1.0 by @dnwe in https://github.com/IBM/sarama/pull/2414
* chore(deps): bump github.com/eapache/go-xerial-snappy digest to bf00bc1 by @dnwe in https://github.com/IBM/sarama/pull/2418

## New Contributors
* @diallo-han made their first contribution in https://github.com/IBM/sarama/pull/2420
* @Lumotheninja made their first contribution in https://github.com/IBM/sarama/pull/2424

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.38.0...v1.38.1

## Version 1.38.0 (2023-01-08)

## What's Changed
### :tada: New Features / Improvements
* feat(producer): improve memory usage of zstd encoder by using our own pool management by @rtreffer in https://github.com/IBM/sarama/pull/2375
* feat(proto): implement and use MetadataRequest v7 by @dnwe in https://github.com/IBM/sarama/pull/2388
* feat(metrics): add protocol-requests-rate metric by @auntan in https://github.com/IBM/sarama/pull/2373
### :bug: Fixes
* fix(proto): track and supply leader epoch to FetchRequest by @dnwe in https://github.com/IBM/sarama/pull/2389
* fix(example): improve arg name used for tls skip verify by @michaeljmarshall in https://github.com/IBM/sarama/pull/2385
* fix(zstd): default back to GOMAXPROCS concurrency by @bgreenlee in https://github.com/IBM/sarama/pull/2404
* fix(producer): add nil check while producer is retrying by @hsweif in https://github.com/IBM/sarama/pull/2387
* fix(producer): return errors for every message in retryBatch to avoid producer hang forever by @cch123 in https://github.com/IBM/sarama/pull/2378
* fix(metrics): fix race when accessing metric registry by @vincentbernat in https://github.com/IBM/sarama/pull/2409
### :package: Dependency updates
* chore(deps): bump golang.org/x/net to v0.4.0 by @dnwe in https://github.com/IBM/sarama/pull/2403
### :wrench: Maintenance
* chore(ci): replace set-output command in GH Action by @dnwe in https://github.com/IBM/sarama/pull/2390
* chore(ci): include kafka 3.3.1 in testing matrix by @dnwe in https://github.com/IBM/sarama/pull/2406

## New Contributors
* @michaeljmarshall made their first contribution in https://github.com/IBM/sarama/pull/2385
* @bgreenlee made their first contribution in https://github.com/IBM/sarama/pull/2404
* @hsweif made their first contribution in https://github.com/IBM/sarama/pull/2387
* @cch123 made their first contribution in https://github.com/IBM/sarama/pull/2378

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.37.2...v1.38.0

## Version 1.37.2 (2022-10-04)

## What's Changed
### :bug: Fixes
* fix: ensure updateMetaDataMs is 64-bit aligned by @dnwe in https://github.com/IBM/sarama/pull/2356
### :heavy_plus_sign: Other Changes
* fix: bump go.mod specification to go 1.17 by @dnwe in https://github.com/IBM/sarama/pull/2357


**Full Changelog**: https://github.com/IBM/sarama/compare/v1.37.1...v1.37.2

## Version 1.37.1 (2022-10-04)

## What's Changed
### :bug: Fixes
* fix: support existing deprecated Rebalance.Strategy field usage by @spongecaptain in https://github.com/IBM/sarama/pull/2352
* fix(test): consumer group rebalance strategy compatibility by @Jacob-bzx in https://github.com/IBM/sarama/pull/2353
* fix(producer): replace time.After with time.Timer to avoid high memory usage by @Jacob-bzx in https://github.com/IBM/sarama/pull/2355

## New Contributors
* @spongecaptain made their first contribution in https://github.com/IBM/sarama/pull/2352

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.37.0...v1.37.1

## Version 1.37.0 (2022-09-28)

## What's Changed

### :rotating_light: Breaking Changes
* Due to a change in [github.com/klauspost/compress v1.15.10](https://github.com/klauspost/compress/releases/tag/v1.15.10), Sarama v1.37.0 requires Go 1.17 going forward, unfortunately due to an oversight this wasn't reflected in the go.mod declaration at time of release.

### :tada: New Features / Improvements
* feat(consumer): support multiple balance strategies by @Jacob-bzx in https://github.com/IBM/sarama/pull/2339
* feat(producer): transactional API by @ryarnyah in https://github.com/IBM/sarama/pull/2295
* feat(mocks): support key in MockFetchResponse. by @Skandalik in https://github.com/IBM/sarama/pull/2328
### :bug: Fixes
* fix: avoid panic when Metadata.RefreshFrequency is 0 by @Jacob-bzx in https://github.com/IBM/sarama/pull/2329
* fix(consumer): avoid pushing unrelated responses to paused children by @pkoutsovasilis in https://github.com/IBM/sarama/pull/2317
* fix: prevent metrics leak with cleanup by @auntan in https://github.com/IBM/sarama/pull/2340
* fix: race condition(may panic) when closing consumer group by @Jacob-bzx in https://github.com/IBM/sarama/pull/2331
* fix(consumer): default ResetInvalidOffsets to true by @dnwe in https://github.com/IBM/sarama/pull/2345
* Validate the `Config` when creating a mock producer/consumer by @joewreschnig in https://github.com/IBM/sarama/pull/2327
### :package: Dependency updates
* chore(deps): bump module github.com/pierrec/lz4/v4 to v4.1.16 by @dnwe in https://github.com/IBM/sarama/pull/2335
* chore(deps): bump golang.org/x/net digest to bea034e by @dnwe in https://github.com/IBM/sarama/pull/2333
* chore(deps): bump golang.org/x/sync digest to 7f9b162 by @dnwe in https://github.com/IBM/sarama/pull/2334
* chore(deps): bump golang.org/x/net digest to f486391 by @dnwe in https://github.com/IBM/sarama/pull/2348
* chore(deps): bump module github.com/shopify/toxiproxy/v2 to v2.5.0 by @dnwe in https://github.com/IBM/sarama/pull/2336
* chore(deps): bump module github.com/klauspost/compress to v1.15.11 by @dnwe in https://github.com/IBM/sarama/pull/2349
* chore(deps): bump module github.com/pierrec/lz4/v4 to v4.1.17 by @dnwe in https://github.com/IBM/sarama/pull/2350
### :wrench: Maintenance
* chore(ci): bump kafka-versions to latest by @dnwe in https://github.com/IBM/sarama/pull/2346
* chore(ci): bump go-versions to N and N-1 by @dnwe in https://github.com/IBM/sarama/pull/2347

## New Contributors
* @Jacob-bzx made their first contribution in https://github.com/IBM/sarama/pull/2329
* @pkoutsovasilis made their first contribution in https://github.com/IBM/sarama/pull/2317
* @Skandalik made their first contribution in https://github.com/IBM/sarama/pull/2328
* @auntan made their first contribution in https://github.com/IBM/sarama/pull/2340
* @ryarnyah made their first contribution in https://github.com/IBM/sarama/pull/2295

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.36.0...v1.37.0

## Version 1.36.0 (2022-08-11)

## What's Changed
### :tada: New Features / Improvements
* feat: add option to propagate OffsetOutOfRange error by @dkolistratova in https://github.com/IBM/sarama/pull/2252
* feat(producer): expose ProducerMessage.byteSize() function by @k8scat in https://github.com/IBM/sarama/pull/2315
* feat(metrics): track consumer fetch request rates by @dnwe in https://github.com/IBM/sarama/pull/2299
### :bug: Fixes
* fix(consumer): avoid submitting empty fetch requests when paused by @raulnegreiros in https://github.com/IBM/sarama/pull/2143
### :package: Dependency updates
* chore(deps): bump module github.com/klauspost/compress to v1.15.9 by @dnwe in https://github.com/IBM/sarama/pull/2304
* chore(deps): bump golang.org/x/net digest to c7608f3 by @dnwe in https://github.com/IBM/sarama/pull/2301
* chore(deps): bump golangci/golangci-lint-action action to v3 by @dnwe in https://github.com/IBM/sarama/pull/2311
* chore(deps): bump golang.org/x/net digest to 07c6da5 by @dnwe in https://github.com/IBM/sarama/pull/2307
* chore(deps): bump github actions versions (major) by @dnwe in https://github.com/IBM/sarama/pull/2313
* chore(deps): bump module github.com/jcmturner/gofork to v1.7.6 by @dnwe in https://github.com/IBM/sarama/pull/2305
* chore(deps): bump golang.org/x/sync digest to 886fb93 by @dnwe in https://github.com/IBM/sarama/pull/2302
* chore(deps): bump module github.com/jcmturner/gokrb5/v8 to v8.4.3 by @dnwe in https://github.com/IBM/sarama/pull/2303
### :wrench: Maintenance
* chore: add kafka 3.1.1 to the version matrix by @dnwe in https://github.com/IBM/sarama/pull/2300
### :heavy_plus_sign: Other Changes
* Migrate off probot-CLA to new GitHub Action by @cursedcoder in https://github.com/IBM/sarama/pull/2294
* Forgot to remove cla probot by @cursedcoder in https://github.com/IBM/sarama/pull/2297
* chore(lint): re-enable a small amount of go-critic by @dnwe in https://github.com/IBM/sarama/pull/2312

## New Contributors
* @cursedcoder made their first contribution in https://github.com/IBM/sarama/pull/2294
* @dkolistratova made their first contribution in https://github.com/IBM/sarama/pull/2252
* @k8scat made their first contribution in https://github.com/IBM/sarama/pull/2315

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.35.0...v1.36.0

## Version 1.35.0 (2022-07-22)

## What's Changed
### :bug: Fixes
* fix: fix metadata retry backoff invalid when get metadata failed by @Stephan14 in https://github.com/IBM/sarama/pull/2256
* fix(balance): sort and de-deplicate memberIDs by @dnwe in https://github.com/IBM/sarama/pull/2285
* fix: prevent DescribeLogDirs hang in admin client by @zerowidth in https://github.com/IBM/sarama/pull/2269
* fix: include assignment-less members in SyncGroup by @dnwe in https://github.com/IBM/sarama/pull/2292
### :package: Dependency updates
* chore(deps): bump module github.com/stretchr/testify to v1.8.0 by @dnwe in https://github.com/IBM/sarama/pull/2284
* chore(deps): bump module github.com/eapache/go-resiliency to v1.3.0 by @dnwe in https://github.com/IBM/sarama/pull/2283
* chore(deps): bump golang.org/x/net digest to 1185a90 by @dnwe in https://github.com/IBM/sarama/pull/2279
* chore(deps): bump module github.com/pierrec/lz4/v4 to v4.1.15 by @dnwe in https://github.com/IBM/sarama/pull/2281
* chore(deps): bump module github.com/klauspost/compress to v1.15.8 by @dnwe in https://github.com/IBM/sarama/pull/2280
### :wrench: Maintenance
* chore: rename `any` func to avoid identifier by @dnwe in https://github.com/IBM/sarama/pull/2272
* chore: add and test against kafka 3.2.0 by @dnwe in https://github.com/IBM/sarama/pull/2288
* chore: document Fetch protocol fields by @dnwe in https://github.com/IBM/sarama/pull/2289
### :heavy_plus_sign: Other Changes
* chore(ci): fix redirect with GITHUB_STEP_SUMMARY by @dnwe in https://github.com/IBM/sarama/pull/2286
* fix(test): permit ECONNRESET in TestInitProducerID by @dnwe in https://github.com/IBM/sarama/pull/2287
* fix: ensure empty or devel version valid by @dnwe in https://github.com/IBM/sarama/pull/2291

## New Contributors
* @zerowidth made their first contribution in https://github.com/IBM/sarama/pull/2269

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.34.1...v1.35.0

##  Version 1.34.1 (2022-06-07)

## What's Changed
### :bug: Fixes
* fix(examples): check session.Context().Done() in examples/consumergroup by @zxc111 in https://github.com/IBM/sarama/pull/2240
* fix(protocol): move AuthorizedOperations into GroupDescription of DescribeGroupsResponse by @aiquestion in https://github.com/IBM/sarama/pull/2247
* fix(protocol): tidyup DescribeGroupsResponse by @dnwe in https://github.com/IBM/sarama/pull/2248
* fix(consumer): range balance strategy not like reference by @njhartwell in https://github.com/IBM/sarama/pull/2245
### :wrench: Maintenance
* chore(ci): experiment with using tparse by @dnwe in https://github.com/IBM/sarama/pull/2236
* chore(deps): bump thirdparty dependencies to latest releases by @dnwe in https://github.com/IBM/sarama/pull/2242

## New Contributors
* @zxc111 made their first contribution in https://github.com/IBM/sarama/pull/2240
* @njhartwell made their first contribution in https://github.com/IBM/sarama/pull/2245

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.34.0...v1.34.1

## Version 1.34.0 (2022-05-30)

## What's Changed
### :tada: New Features / Improvements
* KIP-345: support static membership by @aiquestion in https://github.com/IBM/sarama/pull/2230
### :bug: Fixes
* fix: KIP-368 use receiver goroutine to process all sasl v1 responses by @k-wall in https://github.com/IBM/sarama/pull/2234
### :wrench: Maintenance
* chore(deps): bump module github.com/pierrec/lz4 to v4 by @dnwe in https://github.com/IBM/sarama/pull/2231
* chore(deps): bump golang.org/x/net digest to 2e3eb7b by @dnwe in https://github.com/IBM/sarama/pull/2232

## New Contributors
* @aiquestion made their first contribution in https://github.com/IBM/sarama/pull/2230

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.33.0...v1.34.0

## Version 1.33.0 (2022-05-11)

## What's Changed
### :rotating_light: Breaking Changes

**Note: with this change, the user of Sarama is required to use Go 1.13's errors.Is etc (rather then ==) when forming conditionals returned by this library.**
* feat: make `ErrOutOfBrokers` wrap the underlying error that prevented connections to the brokers by @k-wall in https://github.com/IBM/sarama/pull/2131


### :tada: New Features / Improvements
* feat(message): add UnmarshalText method to CompressionCodec by @vincentbernat in https://github.com/IBM/sarama/pull/2172
* KIP-368 : Allow SASL Connections to Periodically Re-Authenticate by @k-wall in https://github.com/IBM/sarama/pull/2197
* feat: add batched CreateACLs func to ClusterAdmin by @nkostoulas in https://github.com/IBM/sarama/pull/2191
### :bug: Fixes
* fix: TestRecordBatchDecoding failing sporadically by @k-wall in https://github.com/IBM/sarama/pull/2154
* feat(test): add an fvt for broker deadlock by @dnwe in https://github.com/IBM/sarama/pull/2144
* fix: avoid starvation in subscriptionManager by @dnwe in https://github.com/IBM/sarama/pull/2109
* fix: remove "Is your cluster reachable?" from msg by @dnwe in https://github.com/IBM/sarama/pull/2165
* fix: remove trailing fullstop from error strings by @dnwe in https://github.com/IBM/sarama/pull/2166
* fix: return underlying sasl error message by @dnwe in https://github.com/IBM/sarama/pull/2164
* fix: potential data race on a global variable by @pior in https://github.com/IBM/sarama/pull/2171
* fix: AdminClient | CreateACLs | check for error in response, return error if needed by @omris94 in https://github.com/IBM/sarama/pull/2185
* producer: ensure that the management message (fin) is never "leaked" by @niamster in https://github.com/IBM/sarama/pull/2182
* fix: prevent RefreshBrokers leaking old brokers  by @k-wall in https://github.com/IBM/sarama/pull/2203
* fix: prevent RefreshController leaking controller by @k-wall in https://github.com/IBM/sarama/pull/2204
* fix: prevent AsyncProducer retryBatch from leaking  by @k-wall in https://github.com/IBM/sarama/pull/2208
* fix: prevent metrics leak when authenticate fails  by @Stephan14 in https://github.com/IBM/sarama/pull/2205
* fix: prevent deadlock between subscription manager and consumer goroutines by @niamster in https://github.com/IBM/sarama/pull/2194
* fix: prevent idempotent producer epoch exhaustion by @ladislavmacoun in https://github.com/IBM/sarama/pull/2178
* fix(test): mockbroker offsetResponse vers behavior by @dnwe in https://github.com/IBM/sarama/pull/2213
* fix: cope with OffsetsLoadInProgress on Join+Sync  by @dnwe in https://github.com/IBM/sarama/pull/2214
* fix: make default MaxWaitTime 500ms by @dnwe in https://github.com/IBM/sarama/pull/2227
### :package: Dependency updates
* chore(deps): bump xdg-go/scram and klauspost/compress by @dnwe in https://github.com/IBM/sarama/pull/2170
### :wrench: Maintenance
* fix(test): skip TestReadOnlyAndAllCommittedMessages by @dnwe in https://github.com/IBM/sarama/pull/2161
* fix(test): remove t.Parallel() by @dnwe in https://github.com/IBM/sarama/pull/2162
* chore(ci): bump along to Go 1.17+1.18 and bump golangci-lint by @dnwe in https://github.com/IBM/sarama/pull/2183
* chore: switch to multi-arch compatible docker images by @dnwe in https://github.com/IBM/sarama/pull/2210
### :heavy_plus_sign: Other Changes
* Remediate a number go-routine leaks (mainly test issues) by @k-wall in https://github.com/IBM/sarama/pull/2198
* chore: retract v1.32.0 due to #2150 by @dnwe in https://github.com/IBM/sarama/pull/2199
* chore: bump functional test timeout to 12m by @dnwe in https://github.com/IBM/sarama/pull/2200
* fix(admin): make DeleteRecords err consistent by @dnwe in https://github.com/IBM/sarama/pull/2226

## New Contributors
* @k-wall made their first contribution in https://github.com/IBM/sarama/pull/2154
* @pior made their first contribution in https://github.com/IBM/sarama/pull/2171
* @omris94 made their first contribution in https://github.com/IBM/sarama/pull/2185
* @vincentbernat made their first contribution in https://github.com/IBM/sarama/pull/2172
* @niamster made their first contribution in https://github.com/IBM/sarama/pull/2182
* @ladislavmacoun made their first contribution in https://github.com/IBM/sarama/pull/2178
* @nkostoulas made their first contribution in https://github.com/IBM/sarama/pull/2191

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.32.0...v1.33.0

## Version 1.32.0 (2022-02-24)

### ⚠️ This release has been superseded by v1.33.0 and should _not_ be used.

* chore: retract v1.32.0 due to #2150 by @dnwe in https://github.com/IBM/sarama/pull/2199

---

## What's Changed
### :bug: Fixes
* Fix deadlock when closing Broker in brokerProducer by @slaunay in https://github.com/IBM/sarama/pull/2133
### :package: Dependency updates
* chore: refresh dependencies to latest by @dnwe in https://github.com/IBM/sarama/pull/2159
### :wrench: Maintenance
* fix: rework RebalancingMultiplePartitions test by @dnwe in https://github.com/IBM/sarama/pull/2130
* fix(test): use Sarama transactional producer by @dnwe in https://github.com/IBM/sarama/pull/1939
* chore: enable t.Parallel() wherever possible by @dnwe in https://github.com/IBM/sarama/pull/2138
### :heavy_plus_sign: Other Changes
* chore: restrict to 1 testbinary at once by @dnwe in https://github.com/IBM/sarama/pull/2145
* chore: restrict to 1 parallel test at once by @dnwe in https://github.com/IBM/sarama/pull/2146
* Remove myself from codeowners by @bai in https://github.com/IBM/sarama/pull/2147
* chore: add retractions for known bad versions by @dnwe in https://github.com/IBM/sarama/pull/2160


**Full Changelog**: https://github.com/IBM/sarama/compare/v1.31.1...v1.32.0

## Version 1.31.1 (2022-02-01)

- #2126 - @bai - Populate missing kafka versions
- #2124 - @bai - Add Kafka 3.1.0 to CI matrix, migrate to bitnami kafka image
- #2123 - @bai - Update klauspost/compress to 0.14
- #2122 - @dnwe - fix(test): make it simpler to re-use toxiproxy
- #2119 - @bai - Add Kafka 3.1.0 version number
- #2005 - @raulnegreiros - feat: add methods to pause/resume consumer's consumption
- #2051 - @seveas - Expose the TLS connection state of a broker connection
- #2117 - @wuhuizuo - feat: add method MockApiVersionsResponse.SetApiKeys
- #2110 - @dnwe - fix: ensure heartbeats only stop after cleanup
- #2113 - @mosceo - Fix typo

## Version 1.31.0 (2022-01-18)

## What's Changed
### :tada: New Features / Improvements
* feat: expose IncrementalAlterConfigs API in admin.go by @fengyinqiao in https://github.com/IBM/sarama/pull/2088
* feat: allow AsyncProducer to have MaxOpenRequests inflight produce requests per broker by @xujianhai666 in https://github.com/IBM/sarama/pull/1686
* Support request pipelining in AsyncProducer by @slaunay in https://github.com/IBM/sarama/pull/2094
### :bug: Fixes
* fix(test): add fluent interface for mocks where missing by @grongor in https://github.com/IBM/sarama/pull/2080
* fix(test): test for ConsumePartition with OffsetOldest by @grongor in https://github.com/IBM/sarama/pull/2081
* fix: set HWMO during creation of partitionConsumer (fix incorrect HWMO before first fetch) by @grongor in https://github.com/IBM/sarama/pull/2082
* fix: ignore non-nil but empty error strings in Describe/Alter client quotas responses by @agriffaut in https://github.com/IBM/sarama/pull/2096
* fix: skip over KIP-482 tagged fields by @dnwe in https://github.com/IBM/sarama/pull/2107
* fix: clear preferredReadReplica if broker shutdown by @dnwe in https://github.com/IBM/sarama/pull/2108
* fix(test): correct wrong offsets in mock Consumer by @grongor in https://github.com/IBM/sarama/pull/2078
* fix: correct bugs in DescribeGroupsResponse by @dnwe in https://github.com/IBM/sarama/pull/2111
### :wrench: Maintenance
* chore: bump runtime and test dependencies by @dnwe in https://github.com/IBM/sarama/pull/2100
### :memo: Documentation
* docs: refresh README.md for Kafka 3.0.0 by @dnwe in https://github.com/IBM/sarama/pull/2099
### :heavy_plus_sign: Other Changes
* Fix typo by @mosceo in https://github.com/IBM/sarama/pull/2084

## New Contributors
* @grongor made their first contribution in https://github.com/IBM/sarama/pull/2080
* @fengyinqiao made their first contribution in https://github.com/IBM/sarama/pull/2088
* @xujianhai666 made their first contribution in https://github.com/IBM/sarama/pull/1686
* @mosceo made their first contribution in https://github.com/IBM/sarama/pull/2084

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.30.1...v1.31.0

## Version 1.30.1 (2021-12-04)

## What's Changed
### :tada: New Features / Improvements
* feat(zstd): pass level param through to compress/zstd encoder by @lizthegrey in https://github.com/IBM/sarama/pull/2045
### :bug: Fixes
* fix: set min-go-version to 1.16 by @troyanov in https://github.com/IBM/sarama/pull/2048
* logger: fix debug logs' formatting directives by @utrack in https://github.com/IBM/sarama/pull/2054
* fix: stuck on the batch with zero records length by @pachmu in https://github.com/IBM/sarama/pull/2057
* fix: only update preferredReadReplica if valid by @dnwe in https://github.com/IBM/sarama/pull/2076
### :wrench: Maintenance
* chore: add release notes configuration by @dnwe in https://github.com/IBM/sarama/pull/2046
* chore: confluent platform version bump by @lizthegrey in https://github.com/IBM/sarama/pull/2070

## Notes
* ℹ️ from Sarama 1.30.x onward the minimum version of Go toolchain required is 1.16.x

## New Contributors
* @troyanov made their first contribution in https://github.com/IBM/sarama/pull/2048
* @lizthegrey made their first contribution in https://github.com/IBM/sarama/pull/2045
* @utrack made their first contribution in https://github.com/IBM/sarama/pull/2054
* @pachmu made their first contribution in https://github.com/IBM/sarama/pull/2057

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.30.0...v1.30.1

## Version 1.30.0 (2021-09-29)

⚠️ This release has been superseded by v1.30.1 and should _not_ be used.

**regression**: enabling rackawareness causes severe throughput drops (#2071) — fixed in v1.30.1 via #2076

---

ℹ️ **Note: from Sarama 1.30.0 the minimum version of Go toolchain required is 1.16.x**

---

# New Features / Improvements

- #1983 - @zifengyu - allow configure AllowAutoTopicCreation argument in metadata refresh
- #2000 - @matzew - Using xdg-go module for SCRAM
- #2003 - @gdm85 - feat: add counter metrics for consumer group join/sync and their failures
- #1992 - @zhaomoran - feat: support SaslHandshakeRequest v0 for SCRAM
- #2006 - @faillefer - Add support for DeleteOffsets operation
- #1909 - @agriffaut - KIP-546 Client quota APIs
- #1633 - @aldelucca1 - feat: allow balance strategies to provide initial state
- #1275 - @dnwe - log: add a DebugLogger that proxies to Logger
- #2018 - @dnwe - feat: use DebugLogger reference for goldenpath log
- #2019 - @dnwe - feat: add logging & a metric for producer throttle
- #2023 - @dnwe - feat: add Controller() to ClusterAdmin interface
- #2025 - @dnwe - feat: support ApiVersionsRequest V3 protocol
- #2028 - @dnwe - feat: send ApiVersionsRequest on broker open
- #2034 - @bai - Add support for kafka 3.0.0

# Fixes

- #1990 - @doxsch - fix: correctly pass ValidateOnly through to CreatePartitionsRequest
- #1988 - @LubergAlexander - fix: correct WithCustomFallbackPartitioner implementation
- #2001 - @HurSungYun - docs: inform AsyncProducer Close pitfalls
- #1973 - @qiangmzsx - fix: metrics still taking up too much memory when metrics.UseNilMetrics=true
- #2007 - @bai - Add support for Go 1.17
- #2009 - @dnwe - fix: enable nilerr linter and fix iferr checks
- #2010 - @dnwe - chore: enable exportloopref and misspell linters
- #2013 - @faillefer - fix(test): disable encoded response/request check when map contains multiple elements
- #2015 - @bai - Change default branch to main
- #1718 - @crivera-fastly - fix: correct the error handling in client.InitProducerID()
- #1984 - @null-sleep - fix(test): bump confluentPlatformVersion from 6.1.1 to 6.2.0
- #2016 - @dnwe - chore: replace deprecated Go calls
- #2017 - @dnwe - chore: delete legacy vagrant script
- #2020 - @dnwe - fix(test): remove testLogger from TrackLeader test
- #2024 - @dnwe - chore: bump toxiproxy container to v2.1.5
- #2033 - @bai - Update dependencies
- #2031 - @gdm85 - docs: do not mention buffered messages in sync producer Close method
- #2035 - @dnwe - chore: populate the missing kafka versions
- #2038 - @dnwe - feat: add a fuzzing workflow to github actions

## New Contributors
* @zifengyu made their first contribution in https://github.com/IBM/sarama/pull/1983
* @doxsch made their first contribution in https://github.com/IBM/sarama/pull/1990
* @LubergAlexander made their first contribution in https://github.com/IBM/sarama/pull/1988
* @HurSungYun made their first contribution in https://github.com/IBM/sarama/pull/2001
* @gdm85 made their first contribution in https://github.com/IBM/sarama/pull/2003
* @qiangmzsx made their first contribution in https://github.com/IBM/sarama/pull/1973
* @zhaomoran made their first contribution in https://github.com/IBM/sarama/pull/1992
* @faillefer made their first contribution in https://github.com/IBM/sarama/pull/2006
* @crivera-fastly made their first contribution in https://github.com/IBM/sarama/pull/1718
* @null-sleep made their first contribution in https://github.com/IBM/sarama/pull/1984

**Full Changelog**: https://github.com/IBM/sarama/compare/v1.29.1...v1.30.0

## Version 1.29.1 (2021-06-24)

# New Features / Improvements

- #1966 - @ajanikow - KIP-339: Add Incremental Config updates API
- #1964 - @ajanikow - Add DelegationToken ResourceType

# Fixes

- #1962 - @hanxiaolin - fix(consumer):  call interceptors when MaxProcessingTime expire
- #1971 - @KerryJava - fix  kafka-producer-performance throughput panic
- #1968 - @dnwe - chore: bump golang.org/x versions
- #1956 - @joewreschnig - Allow checking the entire `ProducerMessage` in the mock producers
- #1963 - @dnwe - fix: ensure backoff timer is re-used
- #1949 - @dnwe - fix: explicitly use uint64 for payload length

## Version 1.29.0 (2021-05-07)

### New Features / Improvements

- #1917 - @arkady-emelyanov - KIP-554: Add Broker-side SCRAM Config API
- #1869 - @wyndhblb - zstd: encode+decode performance improvements
- #1541 - @izolight - add String, (Un)MarshalText for acl types.
- #1921 - @bai - Add support for Kafka 2.8.0

### Fixes
- #1936 - @dnwe - fix(consumer): follow preferred broker
- #1933 - @ozzieba - Use gofork for encoding/asn1 to fix ASN errors during Kerberos authentication
- #1929 - @celrenheit - Handle isolation level in Offset(Request|Response) and require stable offset in FetchOffset(Request|Response)
- #1926 - @dnwe - fix: correct initial CodeQL findings
- #1925 - @bai - Test out CodeQL
- #1923 - @bestgopher - Remove redundant switch-case, fix doc typos
- #1922 - @bai - Update go dependencies
- #1898 - @mmaslankaprv - Parsing only known control batches value
- #1887 - @withshubh - Fix: issues affecting code quality

## Version 1.28.0 (2021-02-15)

**Note that with this release we change `RoundRobinBalancer` strategy to match Java client behavior. See #1788 for details.**

- #1870 - @kvch - Update Kerberos library to latest major
- #1876 - @bai - Update docs, reference pkg.go.dev
- #1846 - @wclaeys - Do not ignore Consumer.Offsets.AutoCommit.Enable config on Close
- #1747 - @XSAM - fix: mock sync producer does not handle the offset while sending messages
- #1863 - @bai - Add support for Kafka 2.7.0 + update lz4 and klauspost/compress dependencies
- #1788 - @kzinglzy - feat[balance_strategy]: announcing a new round robin balance strategy
- #1862 - @bai - Fix CI setenv permissions issues
- #1832 - @ilyakaznacheev - Update Godoc link to pkg.go.dev
- #1822 - @danp - KIP-392: Allow consumers to fetch from closest replica

## Version 1.27.2 (2020-10-21)

### Improvements

#1750 - @krantideep95 Adds missing mock responses for mocking consumer group

## Fixes

#1817 - reverts #1785 - Add private method to Client interface to prevent implementation

## Version 1.27.1 (2020-10-07)

### Improvements

#1775 - @d1egoaz - Adds a Producer Interceptor example
#1781 - @justin-chen - Refresh brokers given list of seed brokers
#1784 - @justin-chen - Add randomize seed broker method
#1790 - @d1egoaz - remove example binary
#1798 - @bai - Test against Go 1.15
#1785 - @justin-chen - Add private method to Client interface to prevent implementation
#1802 - @uvw - Support Go 1.13 error unwrapping

## Fixes

#1791 - @stanislavkozlovski - bump default version to 1.0.0

## Version 1.27.0 (2020-08-11)

### Improvements

#1466 - @rubenvp8510  - Expose kerberos fast negotiation configuration
#1695 - @KJTsanaktsidis - Use docker-compose to run the functional tests
#1699 - @wclaeys  - Consumer group support for manually comitting offsets
#1714 - @bai - Bump Go to version 1.14.3, golangci-lint to 1.27.0
#1726 - @d1egoaz - Include zstd on the functional tests
#1730 - @d1egoaz - KIP-42 Add producer and consumer interceptors
#1738 - @varun06 - fixed variable names that are named same as some std lib package names
#1741 - @varun06 - updated zstd dependency to latest v1.10.10
#1743 - @varun06 - Fixed declaration dependencies and other lint issues in code base
#1763 - @alrs - remove deprecated tls options from test
#1769 - @bai - Add support for Kafka 2.6.0

## Fixes

#1697 - @kvch - Use gofork for encoding/asn1 to fix ASN errors during Kerberos authentication
#1744 - @alrs  - Fix isBalanced Function Signature

## Version 1.26.4 (2020-05-19)

## Fixes

- #1701 - @d1egoaz - Set server name only for the current broker
- #1694 - @dnwe - testfix: set KAFKA_HEAP_OPTS for zk and kafka

## Version 1.26.3 (2020-05-07)

## Fixes

- #1692 - @d1egoaz - Set tls ServerName to fix issue: either ServerName or InsecureSkipVerify must be specified in the tls.Config

## Version 1.26.2 (2020-05-06)

## ⚠️ Known Issues

This release has been marked as not ready for production and may be unstable, please use v1.26.4.

### Improvements

- #1560 - @iyacontrol - add sync pool for gzip 1-9
- #1605 - @dnwe - feat: protocol support for V11 fetch w/ rackID
- #1617 - @sladkoff / @dwi-di / @random-dwi - Add support for alter/list partition reassignements APIs
- #1632 - @bai - Add support for Go 1.14
- #1640 - @random-dwi - Feature/fix list partition reassignments
- #1646 - @mimaison - Add DescribeLogDirs to admin client
- #1667 - @bai - Add support for kafka 2.5.0

## Fixes

- #1594 - @sladkoff - Sets ConfigEntry.Default flag in addition to the ConfigEntry.Source for Kafka versions > V1_1_0_0
- #1601 - @alrs - fix: remove use of testing.T.FailNow() inside goroutine
- #1602 - @d1egoaz - adds a note about consumer groups Consume method
- #1607 - @darklore - Fix memory leak when Broker.Open and Broker.Close called repeatedly
- #1613 - @wblakecaldwell - Updated "retrying" log message when BackoffFunc implemented
- #1614 - @alrs - produce_response.go: Remove Unused Functions
- #1619 - @alrs - tools/kafka-producer-performance: prune unused flag variables
- #1639 - @agriffaut - Handle errors with no message but error code
- #1643 - @kzinglzy - fix `config.net.keepalive`
- #1644 - @KJTsanaktsidis - Fix brokers continually allocating new Session IDs
- #1645 - @Stephan14 - Remove broker(s) which no longer exist in metadata
- #1650 - @lavoiesl - Return the response error in heartbeatLoop
- #1661 - @KJTsanaktsidis - Fix "broker received out of order sequence" when brokers die
- #1666 - @KevinJCross - Bugfix: Allow TLS connections to work over socks proxy.

## Version 1.26.1 (2020-02-04)

Improvements:
- Add requests-in-flight metric ([1539](https://github.com/IBM/sarama/pull/1539))
- Fix misleading example for cluster admin ([1595](https://github.com/IBM/sarama/pull/1595))
- Replace Travis with GitHub Actions, linters housekeeping ([1573](https://github.com/IBM/sarama/pull/1573))
- Allow BalanceStrategy to provide custom assignment data ([1592](https://github.com/IBM/sarama/pull/1592))

Bug Fixes:
- Adds back Consumer.Offsets.CommitInterval to fix API ([1590](https://github.com/IBM/sarama/pull/1590))
- Fix error message s/CommitInterval/AutoCommit.Interval ([1589](https://github.com/IBM/sarama/pull/1589))

## Version 1.26.0 (2020-01-24)

New Features:
- Enable zstd compression
  ([1574](https://github.com/IBM/sarama/pull/1574),
  [1582](https://github.com/IBM/sarama/pull/1582))
- Support headers in tools kafka-console-producer
  ([1549](https://github.com/IBM/sarama/pull/1549))

Improvements:
- Add SASL AuthIdentity to SASL frames (authzid)
  ([1585](https://github.com/IBM/sarama/pull/1585)).

Bug Fixes:
- Sending messages with ZStd compression enabled fails in multiple ways
  ([1252](https://github.com/IBM/sarama/issues/1252)).
- Use the broker for any admin on BrokerConfig
  ([1571](https://github.com/IBM/sarama/pull/1571)).
- Set DescribeConfigRequest Version field
  ([1576](https://github.com/IBM/sarama/pull/1576)).
- ConsumerGroup flooding logs with client/metadata update req
  ([1578](https://github.com/IBM/sarama/pull/1578)).
- MetadataRequest version in DescribeCluster
  ([1580](https://github.com/IBM/sarama/pull/1580)).
- Fix deadlock in consumer group handleError
  ([1581](https://github.com/IBM/sarama/pull/1581))
- Fill in the Fetch{Request,Response} protocol
  ([1582](https://github.com/IBM/sarama/pull/1582)).
- Retry topic request on ControllerNotAvailable
  ([1586](https://github.com/IBM/sarama/pull/1586)).

## Version 1.25.0 (2020-01-13)

New Features:
- Support TLS protocol in kafka-producer-performance
  ([1538](https://github.com/IBM/sarama/pull/1538)).
- Add support for kafka 2.4.0
  ([1552](https://github.com/IBM/sarama/pull/1552)).

Improvements:
- Allow the Consumer to disable auto-commit offsets
  ([1164](https://github.com/IBM/sarama/pull/1164)).
- Produce records with consistent timestamps
  ([1455](https://github.com/IBM/sarama/pull/1455)).

Bug Fixes:
- Fix incorrect SetTopicMetadata name mentions
  ([1534](https://github.com/IBM/sarama/pull/1534)).
- Fix client.tryRefreshMetadata Println
  ([1535](https://github.com/IBM/sarama/pull/1535)).
- Fix panic on calling updateMetadata on closed client
  ([1531](https://github.com/IBM/sarama/pull/1531)).
- Fix possible faulty metrics in TestFuncProducing
  ([1545](https://github.com/IBM/sarama/pull/1545)).

## Version 1.24.1 (2019-10-31)

New Features:
- Add DescribeLogDirs Request/Response pair
  ([1520](https://github.com/IBM/sarama/pull/1520)).

Bug Fixes:
- Fix ClusterAdmin returning invalid controller ID on DescribeCluster
  ([1518](https://github.com/IBM/sarama/pull/1518)).
- Fix issue with consumergroup not rebalancing when new partition is added
  ([1525](https://github.com/IBM/sarama/pull/1525)).
- Ensure consistent use of read/write deadlines
  ([1529](https://github.com/IBM/sarama/pull/1529)).

## Version 1.24.0 (2019-10-09)

New Features:
- Add sticky partition assignor
  ([1416](https://github.com/IBM/sarama/pull/1416)).
- Switch from cgo zstd package to pure Go implementation
  ([1477](https://github.com/IBM/sarama/pull/1477)).

Improvements:
- Allow creating ClusterAdmin from client
  ([1415](https://github.com/IBM/sarama/pull/1415)).
- Set KafkaVersion in ListAcls method
  ([1452](https://github.com/IBM/sarama/pull/1452)).
- Set request version in CreateACL ClusterAdmin method
  ([1458](https://github.com/IBM/sarama/pull/1458)).
- Set request version in DeleteACL ClusterAdmin method
  ([1461](https://github.com/IBM/sarama/pull/1461)).
- Handle missed error codes on TopicMetaDataRequest and GroupCoordinatorRequest
  ([1464](https://github.com/IBM/sarama/pull/1464)).
- Remove direct usage of gofork
  ([1465](https://github.com/IBM/sarama/pull/1465)).
- Add support for Go 1.13
  ([1478](https://github.com/IBM/sarama/pull/1478)).
- Improve behavior of NewMockListAclsResponse
  ([1481](https://github.com/IBM/sarama/pull/1481)).

Bug Fixes:
- Fix race condition in consumergroup example
  ([1434](https://github.com/IBM/sarama/pull/1434)).
- Fix brokerProducer goroutine leak
  ([1442](https://github.com/IBM/sarama/pull/1442)).
- Use released version of lz4 library
  ([1469](https://github.com/IBM/sarama/pull/1469)).
- Set correct version in MockDeleteTopicsResponse
  ([1484](https://github.com/IBM/sarama/pull/1484)).
- Fix CLI help message typo
  ([1494](https://github.com/IBM/sarama/pull/1494)).

Known Issues:
- Please **don't** use Zstd, as it doesn't work right now.
  See https://github.com/IBM/sarama/issues/1252

## Version 1.23.1 (2019-07-22)

Bug Fixes:
- Fix fetch delete bug record
  ([1425](https://github.com/IBM/sarama/pull/1425)).
- Handle SASL/OAUTHBEARER token rejection
  ([1428](https://github.com/IBM/sarama/pull/1428)).

## Version 1.23.0 (2019-07-02)

New Features:
- Add support for Kafka 2.3.0
  ([1418](https://github.com/IBM/sarama/pull/1418)).
- Add support for ListConsumerGroupOffsets v2
  ([1374](https://github.com/IBM/sarama/pull/1374)).
- Add support for DeleteConsumerGroup
  ([1417](https://github.com/IBM/sarama/pull/1417)).
- Add support for SASLVersion configuration
  ([1410](https://github.com/IBM/sarama/pull/1410)).
- Add kerberos support
  ([1366](https://github.com/IBM/sarama/pull/1366)).

Improvements:
- Improve sasl_scram_client example
  ([1406](https://github.com/IBM/sarama/pull/1406)).
- Fix shutdown and race-condition in consumer-group example
  ([1404](https://github.com/IBM/sarama/pull/1404)).
- Add support for error codes 77—81
  ([1397](https://github.com/IBM/sarama/pull/1397)).
- Pool internal objects allocated per message
  ([1385](https://github.com/IBM/sarama/pull/1385)).
- Reduce packet decoder allocations
  ([1373](https://github.com/IBM/sarama/pull/1373)).
- Support timeout when fetching metadata
  ([1359](https://github.com/IBM/sarama/pull/1359)).

Bug Fixes:
- Fix fetch size integer overflow
  ([1376](https://github.com/IBM/sarama/pull/1376)).
- Handle and log throttled FetchResponses
  ([1383](https://github.com/IBM/sarama/pull/1383)).
- Refactor misspelled word Resouce to Resource
  ([1368](https://github.com/IBM/sarama/pull/1368)).

## Version 1.22.1 (2019-04-29)

Improvements:
- Use zstd 1.3.8
  ([1350](https://github.com/IBM/sarama/pull/1350)).
- Add support for SaslHandshakeRequest v1
  ([1354](https://github.com/IBM/sarama/pull/1354)).

Bug Fixes:
- Fix V5 MetadataRequest nullable topics array
  ([1353](https://github.com/IBM/sarama/pull/1353)).
- Use a different SCRAM client for each broker connection
  ([1349](https://github.com/IBM/sarama/pull/1349)).
- Fix AllowAutoTopicCreation for MetadataRequest greater than v3
  ([1344](https://github.com/IBM/sarama/pull/1344)).

## Version 1.22.0 (2019-04-09)

New Features:
- Add Offline Replicas Operation to Client
  ([1318](https://github.com/IBM/sarama/pull/1318)).
- Allow using proxy when connecting to broker
  ([1326](https://github.com/IBM/sarama/pull/1326)).
- Implement ReadCommitted
  ([1307](https://github.com/IBM/sarama/pull/1307)).
- Add support for Kafka 2.2.0
  ([1331](https://github.com/IBM/sarama/pull/1331)).
- Add SASL SCRAM-SHA-512 and SCRAM-SHA-256 mechanismes
  ([1331](https://github.com/IBM/sarama/pull/1295)).

Improvements:
- Unregister all broker metrics on broker stop
  ([1232](https://github.com/IBM/sarama/pull/1232)).
- Add SCRAM authentication example
  ([1303](https://github.com/IBM/sarama/pull/1303)).
- Add consumergroup examples
  ([1304](https://github.com/IBM/sarama/pull/1304)).
- Expose consumer batch size metric
  ([1296](https://github.com/IBM/sarama/pull/1296)).
- Add TLS options to console producer and consumer
  ([1300](https://github.com/IBM/sarama/pull/1300)).
- Reduce client close bookkeeping
  ([1297](https://github.com/IBM/sarama/pull/1297)).
- Satisfy error interface in create responses
  ([1154](https://github.com/IBM/sarama/pull/1154)).
- Please lint gods
  ([1346](https://github.com/IBM/sarama/pull/1346)).

Bug Fixes:
- Fix multi consumer group instance crash
  ([1338](https://github.com/IBM/sarama/pull/1338)).
- Update lz4 to latest version
  ([1347](https://github.com/IBM/sarama/pull/1347)).
- Retry ErrNotCoordinatorForConsumer in new consumergroup session
  ([1231](https://github.com/IBM/sarama/pull/1231)).
- Fix cleanup error handler
  ([1332](https://github.com/IBM/sarama/pull/1332)).
- Fix rate condition in PartitionConsumer
  ([1156](https://github.com/IBM/sarama/pull/1156)).

## Version 1.21.0 (2019-02-24)

New Features:
- Add CreateAclRequest, DescribeAclRequest, DeleteAclRequest
  ([1236](https://github.com/IBM/sarama/pull/1236)).
- Add DescribeTopic, DescribeConsumerGroup, ListConsumerGroups, ListConsumerGroupOffsets admin requests
  ([1178](https://github.com/IBM/sarama/pull/1178)).
- Implement SASL/OAUTHBEARER
  ([1240](https://github.com/IBM/sarama/pull/1240)).

Improvements:
- Add Go mod support
  ([1282](https://github.com/IBM/sarama/pull/1282)).
- Add error codes 73—76
  ([1239](https://github.com/IBM/sarama/pull/1239)).
- Add retry backoff function
  ([1160](https://github.com/IBM/sarama/pull/1160)).
- Maintain metadata in the producer even when retries are disabled
  ([1189](https://github.com/IBM/sarama/pull/1189)).
- Include ReplicaAssignment in ListTopics
  ([1274](https://github.com/IBM/sarama/pull/1274)).
- Add producer performance tool
  ([1222](https://github.com/IBM/sarama/pull/1222)).
- Add support LogAppend timestamps
  ([1258](https://github.com/IBM/sarama/pull/1258)).

Bug Fixes:
- Fix potential deadlock when a heartbeat request fails
  ([1286](https://github.com/IBM/sarama/pull/1286)).
- Fix consuming compacted topic
  ([1227](https://github.com/IBM/sarama/pull/1227)).
- Set correct Kafka version for DescribeConfigsRequest v1
  ([1277](https://github.com/IBM/sarama/pull/1277)).
- Update kafka test version
  ([1273](https://github.com/IBM/sarama/pull/1273)).

## Version 1.20.1 (2019-01-10)

New Features:
- Add optional replica id in offset request
  ([1100](https://github.com/IBM/sarama/pull/1100)).

Improvements:
- Implement DescribeConfigs Request + Response v1 & v2
  ([1230](https://github.com/IBM/sarama/pull/1230)).
- Reuse compression objects
  ([1185](https://github.com/IBM/sarama/pull/1185)).
- Switch from png to svg for GoDoc link in README
  ([1243](https://github.com/IBM/sarama/pull/1243)).
- Fix typo in deprecation notice for FetchResponseBlock.Records
  ([1242](https://github.com/IBM/sarama/pull/1242)).
- Fix typos in consumer metadata response file
  ([1244](https://github.com/IBM/sarama/pull/1244)).

Bug Fixes:
- Revert to individual msg retries for non-idempotent
  ([1203](https://github.com/IBM/sarama/pull/1203)).
- Respect MaxMessageBytes limit for uncompressed messages
  ([1141](https://github.com/IBM/sarama/pull/1141)).

## Version 1.20.0 (2018-12-10)

New Features:
 - Add support for zstd compression
   ([#1170](https://github.com/IBM/sarama/pull/1170)).
 - Add support for Idempotent Producer
   ([#1152](https://github.com/IBM/sarama/pull/1152)).
 - Add support support for Kafka 2.1.0
   ([#1229](https://github.com/IBM/sarama/pull/1229)).
 - Add support support for OffsetCommit request/response pairs versions v1 to v5
   ([#1201](https://github.com/IBM/sarama/pull/1201)).
 - Add support support for OffsetFetch request/response pair up to version v5
   ([#1198](https://github.com/IBM/sarama/pull/1198)).

Improvements:
 - Export broker's Rack setting
   ([#1173](https://github.com/IBM/sarama/pull/1173)).
 - Always use latest patch version of Go on CI
   ([#1202](https://github.com/IBM/sarama/pull/1202)).
 - Add error codes 61 to 72
   ([#1195](https://github.com/IBM/sarama/pull/1195)).

Bug Fixes:
 - Fix build without cgo
   ([#1182](https://github.com/IBM/sarama/pull/1182)).
 - Fix go vet suggestion in consumer group file
   ([#1209](https://github.com/IBM/sarama/pull/1209)).
 - Fix typos in code and comments
   ([#1228](https://github.com/IBM/sarama/pull/1228)).

## Version 1.19.0 (2018-09-27)

New Features:
 - Implement a higher-level consumer group
   ([#1099](https://github.com/IBM/sarama/pull/1099)).

Improvements:
 - Add support for Go 1.11
   ([#1176](https://github.com/IBM/sarama/pull/1176)).

Bug Fixes:
 - Fix encoding of `MetadataResponse` with version 2 and higher
   ([#1174](https://github.com/IBM/sarama/pull/1174)).
 - Fix race condition in mock async producer
   ([#1174](https://github.com/IBM/sarama/pull/1174)).

## Version 1.18.0 (2018-09-07)

New Features:
 - Make `Partitioner.RequiresConsistency` vary per-message
   ([#1112](https://github.com/IBM/sarama/pull/1112)).
 - Add customizable partitioner
   ([#1118](https://github.com/IBM/sarama/pull/1118)).
 - Add `ClusterAdmin` support for `CreateTopic`, `DeleteTopic`, `CreatePartitions`,
   `DeleteRecords`, `DescribeConfig`, `AlterConfig`, `CreateACL`, `ListAcls`, `DeleteACL`
   ([#1055](https://github.com/IBM/sarama/pull/1055)).

Improvements:
 - Add support for Kafka 2.0.0
   ([#1149](https://github.com/IBM/sarama/pull/1149)).
 - Allow setting `LocalAddr` when dialing an address to support multi-homed hosts
   ([#1123](https://github.com/IBM/sarama/pull/1123)).
 - Simpler offset management
   ([#1127](https://github.com/IBM/sarama/pull/1127)).

Bug Fixes:
 - Fix mutation of `ProducerMessage.MetaData` when producing to Kafka
   ([#1110](https://github.com/IBM/sarama/pull/1110)).
 - Fix consumer block when response did not contain all the
   expected topic/partition blocks
   ([#1086](https://github.com/IBM/sarama/pull/1086)).
 - Fix consumer block when response contains only constrol messages
   ([#1115](https://github.com/IBM/sarama/pull/1115)).
 - Add timeout config for ClusterAdmin requests
   ([#1142](https://github.com/IBM/sarama/pull/1142)).
 - Add version check when producing message with headers
   ([#1117](https://github.com/IBM/sarama/pull/1117)).
 - Fix `MetadataRequest` for empty list of topics
   ([#1132](https://github.com/IBM/sarama/pull/1132)).
 - Fix producer topic metadata on-demand fetch when topic error happens in metadata response
   ([#1125](https://github.com/IBM/sarama/pull/1125)).

## Version 1.17.0 (2018-05-30)

New Features:
 - Add support for gzip compression levels
   ([#1044](https://github.com/IBM/sarama/pull/1044)).
 - Add support for Metadata request/response pairs versions v1 to v5
   ([#1047](https://github.com/IBM/sarama/pull/1047),
    [#1069](https://github.com/IBM/sarama/pull/1069)).
 - Add versioning to JoinGroup request/response pairs
   ([#1098](https://github.com/IBM/sarama/pull/1098))
 - Add support for CreatePartitions, DeleteGroups, DeleteRecords request/response pairs
   ([#1065](https://github.com/IBM/sarama/pull/1065),
    [#1096](https://github.com/IBM/sarama/pull/1096),
    [#1027](https://github.com/IBM/sarama/pull/1027)).
 - Add `Controller()` method to Client interface
   ([#1063](https://github.com/IBM/sarama/pull/1063)).

Improvements:
 - ConsumerMetadataReq/Resp has been migrated to FindCoordinatorReq/Resp
   ([#1010](https://github.com/IBM/sarama/pull/1010)).
 - Expose missing protocol parts: `msgSet` and `recordBatch`
   ([#1049](https://github.com/IBM/sarama/pull/1049)).
 - Add support for v1 DeleteTopics Request
   ([#1052](https://github.com/IBM/sarama/pull/1052)).
 - Add support for Go 1.10
   ([#1064](https://github.com/IBM/sarama/pull/1064)).
 - Claim support for Kafka 1.1.0
   ([#1073](https://github.com/IBM/sarama/pull/1073)).

Bug Fixes:
 - Fix FindCoordinatorResponse.encode to allow nil Coordinator
   ([#1050](https://github.com/IBM/sarama/pull/1050),
    [#1051](https://github.com/IBM/sarama/pull/1051)).
 - Clear all metadata when we have the latest topic info
   ([#1033](https://github.com/IBM/sarama/pull/1033)).
 - Make `PartitionConsumer.Close` idempotent
   ([#1092](https://github.com/IBM/sarama/pull/1092)).

## Version 1.16.0 (2018-02-12)

New Features:
 - Add support for the Create/Delete Topics request/response pairs
   ([#1007](https://github.com/IBM/sarama/pull/1007),
    [#1008](https://github.com/IBM/sarama/pull/1008)).
 - Add support for the Describe/Create/Delete ACL request/response pairs
   ([#1009](https://github.com/IBM/sarama/pull/1009)).
 - Add support for the five transaction-related request/response pairs
   ([#1016](https://github.com/IBM/sarama/pull/1016)).

Improvements:
 - Permit setting version on mock producer responses
   ([#999](https://github.com/IBM/sarama/pull/999)).
 - Add `NewMockBrokerListener` helper for testing TLS connections
   ([#1019](https://github.com/IBM/sarama/pull/1019)).
 - Changed the default value for `Consumer.Fetch.Default` from 32KiB to 1MiB
   which results in much higher throughput in most cases
   ([#1024](https://github.com/IBM/sarama/pull/1024)).
 - Reuse the `time.Ticker` across fetch requests in the PartitionConsumer to
   reduce CPU and memory usage when processing many partitions
   ([#1028](https://github.com/IBM/sarama/pull/1028)).
 - Assign relative offsets to messages in the producer to save the brokers a
   recompression pass
   ([#1002](https://github.com/IBM/sarama/pull/1002),
    [#1015](https://github.com/IBM/sarama/pull/1015)).

Bug Fixes:
 - Fix producing uncompressed batches with the new protocol format
   ([#1032](https://github.com/IBM/sarama/issues/1032)).
 - Fix consuming compacted topics with the new protocol format
   ([#1005](https://github.com/IBM/sarama/issues/1005)).
 - Fix consuming topics with a mix of protocol formats
   ([#1021](https://github.com/IBM/sarama/issues/1021)).
 - Fix consuming when the broker includes multiple batches in a single response
   ([#1022](https://github.com/IBM/sarama/issues/1022)).
 - Fix detection of `PartialTrailingMessage` when the partial message was
   truncated before the magic value indicating its version
   ([#1030](https://github.com/IBM/sarama/pull/1030)).
 - Fix expectation-checking in the mock of `SyncProducer.SendMessages`
   ([#1035](https://github.com/IBM/sarama/pull/1035)).

## Version 1.15.0 (2017-12-08)

New Features:
 - Claim official support for Kafka 1.0, though it did already work
   ([#984](https://github.com/IBM/sarama/pull/984)).
 - Helper methods for Kafka version numbers to/from strings
   ([#989](https://github.com/IBM/sarama/pull/989)).
 - Implement CreatePartitions request/response
   ([#985](https://github.com/IBM/sarama/pull/985)).

Improvements:
 - Add error codes 45-60
   ([#986](https://github.com/IBM/sarama/issues/986)).

Bug Fixes:
 - Fix slow consuming for certain Kafka 0.11/1.0 configurations
   ([#982](https://github.com/IBM/sarama/pull/982)).
 - Correctly determine when a FetchResponse contains the new message format
   ([#990](https://github.com/IBM/sarama/pull/990)).
 - Fix producing with multiple headers
   ([#996](https://github.com/IBM/sarama/pull/996)).
 - Fix handling of truncated record batches
   ([#998](https://github.com/IBM/sarama/pull/998)).
 - Fix leaking metrics when closing brokers
   ([#991](https://github.com/IBM/sarama/pull/991)).

## Version 1.14.0 (2017-11-13)

New Features:
 - Add support for the new Kafka 0.11 record-batch format, including the wire
   protocol and the necessary behavioural changes in the producer and consumer.
   Transactions and idempotency are not yet supported, but producing and
   consuming should work with all the existing bells and whistles (batching,
   compression, etc) as well as the new custom headers. Thanks to Vlad Hanciuta
   of Arista Networks for this work. Part of
   ([#901](https://github.com/IBM/sarama/issues/901)).

Bug Fixes:
 - Fix encoding of ProduceResponse versions in test
   ([#970](https://github.com/IBM/sarama/pull/970)).
 - Return partial replicas list when we have it
   ([#975](https://github.com/IBM/sarama/pull/975)).

## Version 1.13.0 (2017-10-04)

New Features:
 - Support for FetchRequest version 3
   ([#905](https://github.com/IBM/sarama/pull/905)).
 - Permit setting version on mock FetchResponses
   ([#939](https://github.com/IBM/sarama/pull/939)).
 - Add a configuration option to support storing only minimal metadata for
   extremely large clusters
   ([#937](https://github.com/IBM/sarama/pull/937)).
 - Add `PartitionOffsetManager.ResetOffset` for backtracking tracked offsets
   ([#932](https://github.com/IBM/sarama/pull/932)).

Improvements:
 - Provide the block-level timestamp when consuming compressed messages
   ([#885](https://github.com/IBM/sarama/issues/885)).
 - `Client.Replicas` and `Client.InSyncReplicas` now respect the order returned
   by the broker, which can be meaningful
   ([#930](https://github.com/IBM/sarama/pull/930)).
 - Use a `Ticker` to reduce consumer timer overhead at the cost of higher
   variance in the actual timeout
   ([#933](https://github.com/IBM/sarama/pull/933)).

Bug Fixes:
 - Gracefully handle messages with negative timestamps
   ([#907](https://github.com/IBM/sarama/pull/907)).
 - Raise a proper error when encountering an unknown message version
   ([#940](https://github.com/IBM/sarama/pull/940)).

## Version 1.12.0 (2017-05-08)

New Features:
 - Added support for the `ApiVersions` request and response pair, and Kafka
   version 0.10.2 ([#867](https://github.com/IBM/sarama/pull/867)). Note
   that you still need to specify the Kafka version in the Sarama configuration
   for the time being.
 - Added a `Brokers` method to the Client which returns the complete set of
   active brokers ([#813](https://github.com/IBM/sarama/pull/813)).
 - Added an `InSyncReplicas` method to the Client which returns the set of all
   in-sync broker IDs for the given partition, now that the Kafka versions for
   which this was misleading are no longer in our supported set
   ([#872](https://github.com/IBM/sarama/pull/872)).
 - Added a `NewCustomHashPartitioner` method which allows constructing a hash
   partitioner with a custom hash method in case the default (FNV-1a) is not
   suitable
   ([#837](https://github.com/IBM/sarama/pull/837),
    [#841](https://github.com/IBM/sarama/pull/841)).

Improvements:
 - Recognize more Kafka error codes
   ([#859](https://github.com/IBM/sarama/pull/859)).

Bug Fixes:
 - Fix an issue where decoding a malformed FetchRequest would not return the
   correct error ([#818](https://github.com/IBM/sarama/pull/818)).
 - Respect ordering of group protocols in JoinGroupRequests. This fix is
   transparent if you're using the `AddGroupProtocol` or
   `AddGroupProtocolMetadata` helpers; otherwise you will need to switch from
   the `GroupProtocols` field (now deprecated) to use `OrderedGroupProtocols`
   ([#812](https://github.com/IBM/sarama/issues/812)).
 - Fix an alignment-related issue with atomics on 32-bit architectures
   ([#859](https://github.com/IBM/sarama/pull/859)).

## Version 1.11.0 (2016-12-20)

_Important:_ As of Sarama 1.11 it is necessary to set the config value of
`Producer.Return.Successes` to true in order to use the SyncProducer. Previous
versions would silently override this value when instantiating a SyncProducer
which led to unexpected values and data races.

New Features:
 - Metrics! Thanks to Sébastien Launay for all his work on this feature
   ([#701](https://github.com/IBM/sarama/pull/701),
    [#746](https://github.com/IBM/sarama/pull/746),
    [#766](https://github.com/IBM/sarama/pull/766)).
 - Add support for LZ4 compression
   ([#786](https://github.com/IBM/sarama/pull/786)).
 - Add support for ListOffsetRequest v1 and Kafka 0.10.1
   ([#775](https://github.com/IBM/sarama/pull/775)).
 - Added a `HighWaterMarks` method to the Consumer which aggregates the
   `HighWaterMarkOffset` values of its child topic/partitions
   ([#769](https://github.com/IBM/sarama/pull/769)).

Bug Fixes:
 - Fixed producing when using timestamps, compression and Kafka 0.10
   ([#759](https://github.com/IBM/sarama/pull/759)).
 - Added missing decoder methods to DescribeGroups response
   ([#756](https://github.com/IBM/sarama/pull/756)).
 - Fix producer shutdown when `Return.Errors` is disabled
   ([#787](https://github.com/IBM/sarama/pull/787)).
 - Don't mutate configuration in SyncProducer
   ([#790](https://github.com/IBM/sarama/pull/790)).
 - Fix crash on SASL initialization failure
   ([#795](https://github.com/IBM/sarama/pull/795)).

## Version 1.10.1 (2016-08-30)

Bug Fixes:
 - Fix the documentation for `HashPartitioner` which was incorrect
   ([#717](https://github.com/IBM/sarama/pull/717)).
 - Permit client creation even when it is limited by ACLs
   ([#722](https://github.com/IBM/sarama/pull/722)).
 - Several fixes to the consumer timer optimization code, regressions introduced
   in v1.10.0. Go's timers are finicky
   ([#730](https://github.com/IBM/sarama/pull/730),
    [#733](https://github.com/IBM/sarama/pull/733),
    [#734](https://github.com/IBM/sarama/pull/734)).
 - Handle consuming compressed relative offsets with Kafka 0.10
   ([#735](https://github.com/IBM/sarama/pull/735)).

## Version 1.10.0 (2016-08-02)

_Important:_ As of Sarama 1.10 it is necessary to tell Sarama the version of
Kafka you are running against (via the `config.Version` value) in order to use
features that may not be compatible with old Kafka versions. If you don't
specify this value it will default to 0.8.2 (the minimum supported), and trying
to use more recent features (like the offset manager) will fail with an error.

_Also:_ The offset-manager's behaviour has been changed to match the upstream
java consumer (see [#705](https://github.com/IBM/sarama/pull/705) and
[#713](https://github.com/IBM/sarama/pull/713)). If you use the
offset-manager, please ensure that you are committing one *greater* than the
last consumed message offset or else you may end up consuming duplicate
messages.

New Features:
 - Support for Kafka 0.10
   ([#672](https://github.com/IBM/sarama/pull/672),
    [#678](https://github.com/IBM/sarama/pull/678),
    [#681](https://github.com/IBM/sarama/pull/681), and others).
 - Support for configuring the target Kafka version
   ([#676](https://github.com/IBM/sarama/pull/676)).
 - Batch producing support in the SyncProducer
   ([#677](https://github.com/IBM/sarama/pull/677)).
 - Extend producer mock to allow setting expectations on message contents
   ([#667](https://github.com/IBM/sarama/pull/667)).

Improvements:
 - Support `nil` compressed messages for deleting in compacted topics
   ([#634](https://github.com/IBM/sarama/pull/634)).
 - Pre-allocate decoding errors, greatly reducing heap usage and GC time against
   misbehaving brokers ([#690](https://github.com/IBM/sarama/pull/690)).
 - Re-use consumer expiry timers, removing one allocation per consumed message
   ([#707](https://github.com/IBM/sarama/pull/707)).

Bug Fixes:
 - Actually default the client ID to "sarama" like we say we do
   ([#664](https://github.com/IBM/sarama/pull/664)).
 - Fix a rare issue where `Client.Leader` could return the wrong error
   ([#685](https://github.com/IBM/sarama/pull/685)).
 - Fix a possible tight loop in the consumer
   ([#693](https://github.com/IBM/sarama/pull/693)).
 - Match upstream's offset-tracking behaviour
   ([#705](https://github.com/IBM/sarama/pull/705)).
 - Report UnknownTopicOrPartition errors from the offset manager
   ([#706](https://github.com/IBM/sarama/pull/706)).
 - Fix possible negative partition value from the HashPartitioner
   ([#709](https://github.com/IBM/sarama/pull/709)).

## Version 1.9.0 (2016-05-16)

New Features:
 - Add support for custom offset manager retention durations
   ([#602](https://github.com/IBM/sarama/pull/602)).
 - Publish low-level mocks to enable testing of third-party producer/consumer
   implementations ([#570](https://github.com/IBM/sarama/pull/570)).
 - Declare support for Golang 1.6
   ([#611](https://github.com/IBM/sarama/pull/611)).
 - Support for SASL plain-text auth
   ([#648](https://github.com/IBM/sarama/pull/648)).

Improvements:
 - Simplified broker locking scheme slightly
   ([#604](https://github.com/IBM/sarama/pull/604)).
 - Documentation cleanup
   ([#605](https://github.com/IBM/sarama/pull/605),
    [#621](https://github.com/IBM/sarama/pull/621),
    [#654](https://github.com/IBM/sarama/pull/654)).

Bug Fixes:
 - Fix race condition shutting down the OffsetManager
   ([#658](https://github.com/IBM/sarama/pull/658)).

## Version 1.8.0 (2016-02-01)

New Features:
 - Full support for Kafka 0.9:
   - All protocol messages and fields
   ([#586](https://github.com/IBM/sarama/pull/586),
   [#588](https://github.com/IBM/sarama/pull/588),
   [#590](https://github.com/IBM/sarama/pull/590)).
   - Verified that TLS support works
   ([#581](https://github.com/IBM/sarama/pull/581)).
   - Fixed the OffsetManager compatibility
   ([#585](https://github.com/IBM/sarama/pull/585)).

Improvements:
 - Optimize for fewer system calls when reading from the network
   ([#584](https://github.com/IBM/sarama/pull/584)).
 - Automatically retry `InvalidMessage` errors to match upstream behaviour
   ([#589](https://github.com/IBM/sarama/pull/589)).

## Version 1.7.0 (2015-12-11)

New Features:
 - Preliminary support for Kafka 0.9
   ([#572](https://github.com/IBM/sarama/pull/572)). This comes with several
   caveats:
   - Protocol-layer support is mostly in place
     ([#577](https://github.com/IBM/sarama/pull/577)), however Kafka 0.9
     renamed some messages and fields, which we did not in order to preserve API
     compatibility.
   - The producer and consumer work against 0.9, but the offset manager does
     not ([#573](https://github.com/IBM/sarama/pull/573)).
   - TLS support may or may not work
     ([#581](https://github.com/IBM/sarama/pull/581)).

Improvements:
 - Don't wait for request timeouts on dead brokers, greatly speeding recovery
   when the TCP connection is left hanging
   ([#548](https://github.com/IBM/sarama/pull/548)).
 - Refactored part of the producer. The new version provides a much more elegant
   solution to [#449](https://github.com/IBM/sarama/pull/449). It is also
   slightly more efficient, and much more precise in calculating batch sizes
   when compression is used
   ([#549](https://github.com/IBM/sarama/pull/549),
   [#550](https://github.com/IBM/sarama/pull/550),
   [#551](https://github.com/IBM/sarama/pull/551)).

Bug Fixes:
 - Fix race condition in consumer test mock
   ([#553](https://github.com/IBM/sarama/pull/553)).

## Version 1.6.1 (2015-09-25)

Bug Fixes:
 - Fix panic that could occur if a user-supplied message value failed to encode
   ([#449](https://github.com/IBM/sarama/pull/449)).

## Version 1.6.0 (2015-09-04)

New Features:
 - Implementation of a consumer offset manager using the APIs introduced in
   Kafka 0.8.2. The API is designed mainly for integration into a future
   high-level consumer, not for direct use, although it is *possible* to use it
   directly.
   ([#461](https://github.com/IBM/sarama/pull/461)).

Improvements:
 - CRC32 calculation is much faster on machines with SSE4.2 instructions,
   removing a major hotspot from most profiles
   ([#255](https://github.com/IBM/sarama/pull/255)).

Bug Fixes:
 - Make protocol decoding more robust against some malformed packets generated
   by go-fuzz ([#523](https://github.com/IBM/sarama/pull/523),
   [#525](https://github.com/IBM/sarama/pull/525)) or found in other ways
   ([#528](https://github.com/IBM/sarama/pull/528)).
 - Fix a potential race condition panic in the consumer on shutdown
   ([#529](https://github.com/IBM/sarama/pull/529)).

## Version 1.5.0 (2015-08-17)

New Features:
 - TLS-encrypted network connections are now supported. This feature is subject
   to change when Kafka releases built-in TLS support, but for now this is
   enough to work with TLS-terminating proxies
   ([#154](https://github.com/IBM/sarama/pull/154)).

Improvements:
 - The consumer will not block if a single partition is not drained by the user;
   all other partitions will continue to consume normally
   ([#485](https://github.com/IBM/sarama/pull/485)).
 - Formatting of error strings has been much improved
   ([#495](https://github.com/IBM/sarama/pull/495)).
 - Internal refactoring of the producer for code cleanliness and to enable
   future work ([#300](https://github.com/IBM/sarama/pull/300)).

Bug Fixes:
 - Fix a potential deadlock in the consumer on shutdown
   ([#475](https://github.com/IBM/sarama/pull/475)).

## Version 1.4.3 (2015-07-21)

Bug Fixes:
 - Don't include the partitioner in the producer's "fetch partitions"
   circuit-breaker ([#466](https://github.com/IBM/sarama/pull/466)).
 - Don't retry messages until the broker is closed when abandoning a broker in
   the producer ([#468](https://github.com/IBM/sarama/pull/468)).
 - Update the import path for snappy-go, it has moved again and the API has
   changed slightly ([#486](https://github.com/IBM/sarama/pull/486)).

## Version 1.4.2 (2015-05-27)

Bug Fixes:
 - Update the import path for snappy-go, it has moved from google code to github
   ([#456](https://github.com/IBM/sarama/pull/456)).

## Version 1.4.1 (2015-05-25)

Improvements:
 - Optimizations when decoding snappy messages, thanks to John Potocny
   ([#446](https://github.com/IBM/sarama/pull/446)).

Bug Fixes:
 - Fix hypothetical race conditions on producer shutdown
   ([#450](https://github.com/IBM/sarama/pull/450),
   [#451](https://github.com/IBM/sarama/pull/451)).

## Version 1.4.0 (2015-05-01)

New Features:
 - The consumer now implements `Topics()` and `Partitions()` methods to enable
   users to dynamically choose what topics/partitions to consume without
   instantiating a full client
   ([#431](https://github.com/IBM/sarama/pull/431)).
 - The partition-consumer now exposes the high water mark offset value returned
   by the broker via the `HighWaterMarkOffset()` method ([#339](https://github.com/IBM/sarama/pull/339)).
 - Added a `kafka-console-consumer` tool capable of handling multiple
   partitions, and deprecated the now-obsolete `kafka-console-partitionConsumer`
   ([#439](https://github.com/IBM/sarama/pull/439),
   [#442](https://github.com/IBM/sarama/pull/442)).

Improvements:
 - The producer's logging during retry scenarios is more consistent, more
   useful, and slightly less verbose
   ([#429](https://github.com/IBM/sarama/pull/429)).
 - The client now shuffles its initial list of seed brokers in order to prevent
   thundering herd on the first broker in the list
   ([#441](https://github.com/IBM/sarama/pull/441)).

Bug Fixes:
 - The producer now correctly manages its state if retries occur when it is
   shutting down, fixing several instances of confusing behaviour and at least
   one potential deadlock ([#419](https://github.com/IBM/sarama/pull/419)).
 - The consumer now handles messages for different partitions asynchronously,
   making it much more resilient to specific user code ordering
   ([#325](https://github.com/IBM/sarama/pull/325)).

## Version 1.3.0 (2015-04-16)

New Features:
 - The client now tracks consumer group coordinators using
   ConsumerMetadataRequests similar to how it tracks partition leadership using
   regular MetadataRequests ([#411](https://github.com/IBM/sarama/pull/411)).
   This adds two methods to the client API:
   - `Coordinator(consumerGroup string) (*Broker, error)`
   - `RefreshCoordinator(consumerGroup string) error`

Improvements:
 - ConsumerMetadataResponses now automatically create a Broker object out of the
   ID/address/port combination for the Coordinator; accessing the fields
   individually has been deprecated
   ([#413](https://github.com/IBM/sarama/pull/413)).
 - Much improved handling of `OffsetOutOfRange` errors in the consumer.
   Consumers will fail to start if the provided offset is out of range
   ([#418](https://github.com/IBM/sarama/pull/418))
   and they will automatically shut down if the offset falls out of range
   ([#424](https://github.com/IBM/sarama/pull/424)).
 - Small performance improvement in encoding and decoding protocol messages
   ([#427](https://github.com/IBM/sarama/pull/427)).

Bug Fixes:
 - Fix a rare race condition in the client's background metadata refresher if
   it happens to be activated while the client is being closed
   ([#422](https://github.com/IBM/sarama/pull/422)).

## Version 1.2.0 (2015-04-07)

Improvements:
 - The producer's behaviour when `Flush.Frequency` is set is now more intuitive
   ([#389](https://github.com/IBM/sarama/pull/389)).
 - The producer is now somewhat more memory-efficient during and after retrying
   messages due to an improved queue implementation
   ([#396](https://github.com/IBM/sarama/pull/396)).
 - The consumer produces much more useful logging output when leadership
   changes ([#385](https://github.com/IBM/sarama/pull/385)).
 - The client's `GetOffset` method will now automatically refresh metadata and
   retry once in the event of stale information or similar
   ([#394](https://github.com/IBM/sarama/pull/394)).
 - Broker connections now have support for using TCP keepalives
   ([#407](https://github.com/IBM/sarama/issues/407)).

Bug Fixes:
 - The OffsetCommitRequest message now correctly implements all three possible
   API versions ([#390](https://github.com/IBM/sarama/pull/390),
   [#400](https://github.com/IBM/sarama/pull/400)).

## Version 1.1.0 (2015-03-20)

Improvements:
 - Wrap the producer's partitioner call in a circuit-breaker so that repeatedly
   broken topics don't choke throughput
   ([#373](https://github.com/IBM/sarama/pull/373)).

Bug Fixes:
 - Fix the producer's internal reference counting in certain unusual scenarios
   ([#367](https://github.com/IBM/sarama/pull/367)).
 - Fix the consumer's internal reference counting in certain unusual scenarios
   ([#369](https://github.com/IBM/sarama/pull/369)).
 - Fix a condition where the producer's internal control messages could have
   gotten stuck ([#368](https://github.com/IBM/sarama/pull/368)).
 - Fix an issue where invalid partition lists would be cached when asking for
   metadata for a non-existant topic ([#372](https://github.com/IBM/sarama/pull/372)).


## Version 1.0.0 (2015-03-17)

Version 1.0.0 is the first tagged version, and is almost a complete rewrite. The primary differences with previous untagged versions are:

- The producer has been rewritten; there is now a `SyncProducer` with a blocking API, and an `AsyncProducer` that is non-blocking.
- The consumer has been rewritten to only open one connection per broker instead of one connection per partition.
- The main types of Sarama are now interfaces to make depedency injection easy; mock implementations for `Consumer`, `SyncProducer` and `AsyncProducer` are provided in the `github.com/IBM/sarama/mocks` package.
- For most uses cases, it is no longer necessary to open a `Client`; this will be done for you.
- All the configuration values have been unified in the `Config` struct.
- Much improved test suite.
