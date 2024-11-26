# Changelog

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
