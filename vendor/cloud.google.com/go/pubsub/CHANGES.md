# Changes

## [1.32.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.31.0...pubsub/v1.32.0) (2023-06-27)


### Features

* **pubsub:** Add push config wrapper fields ([ca94e27](https://github.com/googleapis/google-cloud-go/commit/ca94e2724f9e2610b46aefd0a3b5ddc06102e91b))
* **pubsub:** Add support for cloud storage subscriptions ([#7977](https://github.com/googleapis/google-cloud-go/issues/7977)) ([54218e9](https://github.com/googleapis/google-cloud-go/commit/54218e963bb5a6d47411a490985b54053825064f))
* **pubsub:** Enable project autodetection and detect empty project ([#8168](https://github.com/googleapis/google-cloud-go/issues/8168)) ([c7e05d8](https://github.com/googleapis/google-cloud-go/commit/c7e05d81502b4ff8d92aad4a3d45a3940e0ead9d))
* **pubsub:** Update all direct dependencies ([b340d03](https://github.com/googleapis/google-cloud-go/commit/b340d030f2b52a4ce48846ce63984b28583abde6))


### Bug Fixes

* **pubsub/pstest:** Align fake handling of bqconfig subscription to server behavior ([#8066](https://github.com/googleapis/google-cloud-go/issues/8066)) ([57914ec](https://github.com/googleapis/google-cloud-go/commit/57914ec4d5d2c894edb564c918606feb89bad5bc))
* **pubsub/pstest:** Fix failing bq config test ([#8060](https://github.com/googleapis/google-cloud-go/issues/8060)) ([fb9db66](https://github.com/googleapis/google-cloud-go/commit/fb9db661d49237d25b20544625edc541670f41ad))
* **pubsub:** Fix issue preventing clearing BQ subscription ([#8040](https://github.com/googleapis/google-cloud-go/issues/8040)) ([0366bf3](https://github.com/googleapis/google-cloud-go/commit/0366bf39b90c38d3139d4aa65c0cdaed1a4d80f1))
* **pubsub:** REST query UpdateMask bug ([df52820](https://github.com/googleapis/google-cloud-go/commit/df52820b0e7721954809a8aa8700b93c5662dc9b))
* **pubsub:** Use fieldmask directly instead of field_mask genproto alias ([#8030](https://github.com/googleapis/google-cloud-go/issues/8030)) ([087a5fc](https://github.com/googleapis/google-cloud-go/commit/087a5fca29d2c21f73e336a0ff714294af7af958))


### Documentation

* **pubsub:** Tightened requirements on cloud storage subscription filename suffixes ([1da334c](https://github.com/googleapis/google-cloud-go/commit/1da334c0cbeed9cfb8df0551714721284d164d60))

## [1.31.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.30.1...pubsub/v1.31.0) (2023-05-24)


### Features

* **pubsub:** Expose common errors for easier handling ([#7940](https://github.com/googleapis/google-cloud-go/issues/7940)) ([983105d](https://github.com/googleapis/google-cloud-go/commit/983105dd86d2ee34a09a5fd665d112f31062cf56))


### Bug Fixes

* **pubsub:** Allow clearing of topic schema ([#7980](https://github.com/googleapis/google-cloud-go/issues/7980)) ([46fc060](https://github.com/googleapis/google-cloud-go/commit/46fc06099852412071be8c8a02d0d9416fd08817))
* **pubsub:** Update grpc to v1.55.0 ([1147ce0](https://github.com/googleapis/google-cloud-go/commit/1147ce02a990276ca4f8ab7a1ab65c14da4450ef))

## [1.30.1](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.30.0...pubsub/v1.30.1) (2023-05-03)


### Bug Fixes

* **pubsub/pstest:** Clear Subscription when calling `ClearMessages`. ([6de8eda](https://github.com/googleapis/google-cloud-go/commit/6de8edada13c751ded733e924174e5b46277fcc6))
* **pubsub/pstest:** Start `DeliveryAttempt` at 1 ([2bf6e14](https://github.com/googleapis/google-cloud-go/commit/2bf6e14ef5d04f9ac2be786086538d395a8e7393))


### Documentation

* **pubsub:** Clarify NumGoroutines configures number of streams ([#7874](https://github.com/googleapis/google-cloud-go/issues/7874)) ([8ac4432](https://github.com/googleapis/google-cloud-go/commit/8ac4432e5d3e77a61119943537915230c0e5b7e9))

## [1.30.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.29.0...pubsub/v1.30.0) (2023-03-22)


### Features

* **pubsub:** Update iam and longrunning deps ([91a1f78](https://github.com/googleapis/google-cloud-go/commit/91a1f784a109da70f63b96414bba8a9b4254cddd))


### Bug Fixes

* **pubsub:** Check response of receipt modacks for exactly once delivery ([#7568](https://github.com/googleapis/google-cloud-go/issues/7568)) ([94d0408](https://github.com/googleapis/google-cloud-go/commit/94d040898cc9e85fdac76560765b01cfd019d0b4))

## [1.29.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.28.0...pubsub/v1.29.0) (2023-03-13)


### Features

* **pubsub:** Add google.api.method.signature to update methods ([aeb6fec](https://github.com/googleapis/google-cloud-go/commit/aeb6fecc7fd3f088ff461a0c068ceb9a7ae7b2a3))
* **pubsub:** Add REST client ([06a54a1](https://github.com/googleapis/google-cloud-go/commit/06a54a16a5866cce966547c51e203b9e09a25bc0))
* **pubsub:** Add schema evolution methods and fields ([ee41485](https://github.com/googleapis/google-cloud-go/commit/ee41485860bcbbd09ce4e28ee6ddca81a5f17211))
* **pubsub:** Add support for schema revisions ([#7295](https://github.com/googleapis/google-cloud-go/issues/7295)) ([369b16f](https://github.com/googleapis/google-cloud-go/commit/369b16f9525f9ac9a0811c66ce61eda9f6c566e4))
* **pubsub:** Add temporary_failed_ack_ids to ModifyAckDeadlineConfirmation ([aeb6fec](https://github.com/googleapis/google-cloud-go/commit/aeb6fecc7fd3f088ff461a0c068ceb9a7ae7b2a3))
* **pubsub:** Make INTERNAL a retryable error for Pull ([aeb6fec](https://github.com/googleapis/google-cloud-go/commit/aeb6fecc7fd3f088ff461a0c068ceb9a7ae7b2a3))


### Bug Fixes

* **pubsub/pstest:** Fix panic on undelivered message ([#7377](https://github.com/googleapis/google-cloud-go/issues/7377)) ([98dd29d](https://github.com/googleapis/google-cloud-go/commit/98dd29d372073605145f78f08205a9786c698881))
* **pubsub:** Allow updating topic schema fields individually ([#7362](https://github.com/googleapis/google-cloud-go/issues/7362)) ([f09e059](https://github.com/googleapis/google-cloud-go/commit/f09e059e3203de5294648d7434d5e65626a6dff5))
* **pubsub:** Dont compare revision fields in schema config test ([#7317](https://github.com/googleapis/google-cloud-go/issues/7317)) ([e364f7a](https://github.com/googleapis/google-cloud-go/commit/e364f7abfe3ec8fc20db78abcdaeaaf27d19269c))
* **pubsub:** Fix bug with AckWithResult with exactly once disabled ([#7319](https://github.com/googleapis/google-cloud-go/issues/7319)) ([c88fbdf](https://github.com/googleapis/google-cloud-go/commit/c88fbdf299205e8118b347430cf66540ffa68b27))
* **pubsub:** Pipe revision ID in name in DeleteSchemaRevision ([#7519](https://github.com/googleapis/google-cloud-go/issues/7519)) ([e211635](https://github.com/googleapis/google-cloud-go/commit/e211635216e553a9a6b00f9e8f2c5d2082ff68a8))


### Documentation

* **pubsub:** Add x-ref for ordering messages docs: Clarify subscription expiration policy ([aeb6fec](https://github.com/googleapis/google-cloud-go/commit/aeb6fecc7fd3f088ff461a0c068ceb9a7ae7b2a3))
* **pubsub:** Clarify BigQueryConfig PERMISSION_DENIED state ([aeb6fec](https://github.com/googleapis/google-cloud-go/commit/aeb6fecc7fd3f088ff461a0c068ceb9a7ae7b2a3))
* **pubsub:** Clarify subscription description ([aeb6fec](https://github.com/googleapis/google-cloud-go/commit/aeb6fecc7fd3f088ff461a0c068ceb9a7ae7b2a3))
* **pubsub:** Mark revision_id in CommitSchemaRevisionRequest deprecated ([2fef56f](https://github.com/googleapis/google-cloud-go/commit/2fef56f75a63dc4ff6e0eea56c7b26d4831c8e27))
* **pubsub:** Replacing HTML code with Markdown docs: Fix PullResponse description docs: Fix Pull description ([aeb6fec](https://github.com/googleapis/google-cloud-go/commit/aeb6fecc7fd3f088ff461a0c068ceb9a7ae7b2a3))
* **pubsub:** Update Pub/Sub topic retention limit from 7 days to 31 days ([aeb6fec](https://github.com/googleapis/google-cloud-go/commit/aeb6fecc7fd3f088ff461a0c068ceb9a7ae7b2a3))

## [1.28.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.27.1...pubsub/v1.28.0) (2022-12-05)


### Features

* **pubsub:** rewrite signatures and type in terms of new location ([620e6d8](https://github.com/googleapis/google-cloud-go/commit/620e6d828ad8641663ae351bfccfe46281e817ad))

## [1.27.1](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.27.0...pubsub/v1.27.1) (2022-12-02)


### Bug Fixes

* **pubsub:** downgrade some dependencies ([7540152](https://github.com/googleapis/google-cloud-go/commit/754015236d5af7c82a75da218b71a87b9ead6eb5))

## [1.27.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.26.0...pubsub/v1.27.0) (2022-11-29)


### Features

* **pubsub:** start generating proto stubs ([cf89415](https://github.com/googleapis/google-cloud-go/commit/cf894154e451a32b431fef2af3781a0d2d8080ff))

## [1.26.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.25.1...pubsub/v1.26.0) (2022-10-24)


### Features

* **pubsub:** Add support for snapshot labels ([#6835](https://github.com/googleapis/google-cloud-go/issues/6835)) ([c17851b](https://github.com/googleapis/google-cloud-go/commit/c17851b5c3d811cd3e6a28162f0e399bb31a1363))


### Bug Fixes

* **pubsub:** Remove unused AckResult map ([#6656](https://github.com/googleapis/google-cloud-go/issues/6656)) ([5f69002](https://github.com/googleapis/google-cloud-go/commit/5f690022551ac584e5c66af4324a17d7044a898d))


### Documentation

* **pubsub:** Fix comments on message for exactly once delivery ([#6878](https://github.com/googleapis/google-cloud-go/issues/6878)) ([a8109e2](https://github.com/googleapis/google-cloud-go/commit/a8109e2d3257d1698ce1b751618428ef25cbb859)), refs [#6877](https://github.com/googleapis/google-cloud-go/issues/6877)
* **pubsub:** Update streams section ([#6682](https://github.com/googleapis/google-cloud-go/issues/6682)) ([7b4e2b4](https://github.com/googleapis/google-cloud-go/commit/7b4e2b412058f965a9f9159231afe551a6f58a74))
* **pubsub:** Update subscription retry policy defaults ([#6909](https://github.com/googleapis/google-cloud-go/issues/6909)) ([c5c2f8f](https://github.com/googleapis/google-cloud-go/commit/c5c2f8f7125034c611edf1d08ca35ece6554c454)), refs [#6903](https://github.com/googleapis/google-cloud-go/issues/6903)

## [1.25.1](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.25.0...pubsub/v1.25.1) (2022-08-24)


### Bug Fixes

* **pubsub:** up version of cloud.google.com/go ([#6558](https://github.com/googleapis/google-cloud-go/issues/6558)) ([be9dcfb](https://github.com/googleapis/google-cloud-go/commit/be9dcfbdfa5876a548eb3c60337c38e1d282bb88)), refs [#6555](https://github.com/googleapis/google-cloud-go/issues/6555)

## [1.25.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.24.0...pubsub/v1.25.0) (2022-08-23)


### Features

* **pubsub:** support exactly once delivery ([#6506](https://github.com/googleapis/google-cloud-go/issues/6506)) ([74da335](https://github.com/googleapis/google-cloud-go/commit/74da335fea6cd70b27808507f2e58ae53f5f4910))


### Documentation

* **pubsub:** typo ([#6453](https://github.com/googleapis/google-cloud-go/issues/6453)) ([34d839e](https://github.com/googleapis/google-cloud-go/commit/34d839ec546633a0fb7f73448337ac8d8c796acd))

## [1.24.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.23.1...pubsub/v1.24.0) (2022-07-18)


### Features

* **pubsub/pstest:** subscription message ordering ([#6257](https://github.com/googleapis/google-cloud-go/issues/6257)) ([71bd273](https://github.com/googleapis/google-cloud-go/commit/71bd273b8a77ed22c41a1284813ee59eb6820bda))


### Bug Fixes

* **pubsub:** make receipt modack call async ([#6335](https://github.com/googleapis/google-cloud-go/issues/6335)) ([d12ca07](https://github.com/googleapis/google-cloud-go/commit/d12ca07720b6b29360e583c1eea22f001239952f))

## [1.23.1](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.23.0...pubsub/v1.23.1) (2022-06-30)


### Bug Fixes

* **pubsub:** increase modack deadline RPC timeout ([#6289](https://github.com/googleapis/google-cloud-go/issues/6289)) ([d24600f](https://github.com/googleapis/google-cloud-go/commit/d24600fda7e574a388e8898c2ecc1958d07f4224))

## [1.23.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.22.2...pubsub/v1.23.0) (2022-06-23)


### Features

* **pubsub:** report publisher outstanding metrics ([#6187](https://github.com/googleapis/google-cloud-go/issues/6187)) ([cc1528b](https://github.com/googleapis/google-cloud-go/commit/cc1528b2bfebbb48d49bcacd639abf2cf3468c96))
* **pubsub:** support bigquery subscriptions ([#6119](https://github.com/googleapis/google-cloud-go/issues/6119)) ([81f704a](https://github.com/googleapis/google-cloud-go/commit/81f704a2cdeece8f73d7c09eae730a905afdb870))

## [1.22.2](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.22.1...pubsub/v1.22.2) (2022-06-03)


### Bug Fixes

* **pubsub:** fix iterator distribution bound calculations ([#6125](https://github.com/googleapis/google-cloud-go/issues/6125)) ([6c470ff](https://github.com/googleapis/google-cloud-go/commit/6c470ff02072d7af32ee07a772c5d0796b545a45))

## [1.22.1](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.22.0...pubsub/v1.22.1) (2022-06-02)


### Bug Fixes

* **pubsub:** use MaxInt instead of MaxInt64 for BufferedByteLimit ([#6113](https://github.com/googleapis/google-cloud-go/issues/6113)) ([06721e0](https://github.com/googleapis/google-cloud-go/commit/06721e06a16f5c94a31b96809aad02f5eb38147c))

## [1.22.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.21.1...pubsub/v1.22.0) (2022-05-31)


### Features

* **pubsub:** add BigQuery configuration for subscriptions ([6ef576e](https://github.com/googleapis/google-cloud-go/commit/6ef576e2d821d079e7b940cd5d49fe3ca64a7ba2))
* **pubsub:** add min extension period ([#6041](https://github.com/googleapis/google-cloud-go/issues/6041)) ([f2407c7](https://github.com/googleapis/google-cloud-go/commit/f2407c7013bbfdfc0103296accc828b0be674f5d))


### Bug Fixes

* **pubsub:** disable deprecated BufferedByteLimit when using MaxOutstandingBytes ([#6009](https://github.com/googleapis/google-cloud-go/issues/6009)) ([dbfdf76](https://github.com/googleapis/google-cloud-go/commit/dbfdf762c77f9cfad637c573b06f0a49e01316f3))

### [1.21.1](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.21.0...pubsub/v1.21.1) (2022-05-04)


### Bug Fixes

* **pubsub:** mark ignore option default for publish flow control ([#5983](https://github.com/googleapis/google-cloud-go/issues/5983)) ([3f41531](https://github.com/googleapis/google-cloud-go/commit/3f41531579b7a55acea66fec8362e9134125c8a0))

## [1.21.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.20.0...pubsub/v1.21.0) (2022-04-26)


### Features

* **pubsub:** deprecate synchronous mode ([#5910](https://github.com/googleapis/google-cloud-go/issues/5910)) ([bda5179](https://github.com/googleapis/google-cloud-go/commit/bda5179fa240b1468cd1043128493f634be28986))


### Bug Fixes

* **pubsub:** enable updating enable_exactly_once_delivery in fake pubsub ([#5940](https://github.com/googleapis/google-cloud-go/issues/5940)) ([ee44bf6](https://github.com/googleapis/google-cloud-go/commit/ee44bf646af1c38ed0943a997051b0225e22a6bf))
* **pubsub:** nack messages properly with error from receive scheduler ([#5909](https://github.com/googleapis/google-cloud-go/issues/5909)) ([80edea4](https://github.com/googleapis/google-cloud-go/commit/80edea40dd722efb3c15cd3de3f24e0e7ad08ed7))

## [1.20.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.19.0...pubsub/v1.20.0) (2022-04-11)


### Features

* **pubsub/pstest:** add topic retention support ([#4790](https://github.com/googleapis/google-cloud-go/issues/4790)) ([0a4ad6a](https://github.com/googleapis/google-cloud-go/commit/0a4ad6a72ddc379a94a88ec70ac678a227843cfd))


### Bug Fixes

* **pubsub:** ignore grpc errors in ack/modack ([#5796](https://github.com/googleapis/google-cloud-go/issues/5796)) ([4fb9aec](https://github.com/googleapis/google-cloud-go/commit/4fb9aecd2bc415c846e26eb960859e10e1af61f3))

## [1.19.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v1.18.0...pubsub/v1.19.0) (2022-03-07)


### Features

* **pubsub:** add better version metadata to calls ([d1ad921](https://github.com/googleapis/google-cloud-go/commit/d1ad921d0322e7ce728ca9d255a3cf0437d26add))
* **pubsub:** set versionClient to module version ([55f0d92](https://github.com/googleapis/google-cloud-go/commit/55f0d92bf112f14b024b4ab0076c9875a17423c9))


### Bug Fixes

* **pubsub:** prevent infinite retry with publishing invalid utf-8 chars ([#5728](https://github.com/googleapis/google-cloud-go/issues/5728)) ([0a4dab9](https://github.com/googleapis/google-cloud-go/commit/0a4dab9043db81342dc41bd496d35fd4a7b08ad5))
* **pubsub:** removing misspelled field, add correctly spelled field ([4a223de](https://github.com/googleapis/google-cloud-go/commit/4a223de8eab072d95818c761e41fb3f3f6ac728c))

## [1.18.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.17.1...pubsub/v1.18.0) (2022-02-08)


### Features

* **pubsub:** add exactly once delivery flag ([f71dc3d](https://www.github.com/googleapis/google-cloud-go/commit/f71dc3dfefa54ab41861aea15971108850a9f98b))
* **pubsub:** add exactly once delivery flag ([f71dc3d](https://www.github.com/googleapis/google-cloud-go/commit/f71dc3dfefa54ab41861aea15971108850a9f98b))


### Bug Fixes

* **pubsub:** add deadletter and retries handling in the fake pubsub ([#5320](https://www.github.com/googleapis/google-cloud-go/issues/5320)) ([116a610](https://www.github.com/googleapis/google-cloud-go/commit/116a61008e174e5d49b9485d78bc13f64461322f))
* **pubsub:** pass context into checkOrdering to allow cancel ([#5316](https://www.github.com/googleapis/google-cloud-go/issues/5316)) ([fc08c49](https://www.github.com/googleapis/google-cloud-go/commit/fc08c49fc013cbad00642bbba317e02f0ba15a6d))

### [1.17.1](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.17.0...pubsub/v1.17.1) (2021-10-25)


### Bug Fixes

* **pubsub:** add methods to allow retrieval of topic/sub config names ([#4953](https://www.github.com/googleapis/google-cloud-go/issues/4953)) ([bff5b1c](https://www.github.com/googleapis/google-cloud-go/commit/bff5b1ca331a0d193407a0f3eb501772cbb8ba78))
* **pubsub:** prevent draining error return for Receive ([#4733](https://www.github.com/googleapis/google-cloud-go/issues/4733)) ([c6d5189](https://www.github.com/googleapis/google-cloud-go/commit/c6d51891649d8169089a0a2b7365ea54f991af56))
* **pubsub:** tag ctx in iterator with subscription for opencensus ([#5011](https://www.github.com/googleapis/google-cloud-go/issues/5011)) ([cdf9588](https://www.github.com/googleapis/google-cloud-go/commit/cdf958864e278bb394cc548cb5f15ad08859f347))

## [1.17.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.16.0...pubsub/v1.17.0) (2021-09-08)


### Features

* **pubsub:** add list configs for topic & sub ([#4607](https://www.github.com/googleapis/google-cloud-go/issues/4607)) ([a6550c5](https://www.github.com/googleapis/google-cloud-go/commit/a6550c5dfb381e286fea6a905dc658c4a865d643))
* **pubsub:** add publisher flow control support ([#4292](https://www.github.com/googleapis/google-cloud-go/issues/4292)) ([bff24c3](https://www.github.com/googleapis/google-cloud-go/commit/bff24c3a62a2f037c1ccef14986f917e41953734))

## [1.16.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.15.0...pubsub/v1.16.0) (2021-08-24)


### Features

* **pubsub:** add topic message retention duration ([#4520](https://www.github.com/googleapis/google-cloud-go/issues/4520)) ([0440336](https://www.github.com/googleapis/google-cloud-go/commit/0440336c988a4401cbdb5d85a8cc7fca388831e5))

## [1.15.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.14.0...pubsub/v1.15.0) (2021-08-13)


### Features

* **pubsub:** Add topic retention options ([5996846](https://www.github.com/googleapis/google-cloud-go/commit/59968462a3870c6289166fa1161f9b6d9c10e093))


### Bug Fixes

* **pubsub:** always make config check to prevent race ([#4606](https://www.github.com/googleapis/google-cloud-go/issues/4606)) ([8cfcf53](https://www.github.com/googleapis/google-cloud-go/commit/8cfcf53d03b9b442e7f0bc1c1b20c791e31c07b0)), refs [#3626](https://www.github.com/googleapis/google-cloud-go/issues/3626)
* **pubsub:** mitigate race in checking ordering config ([#4602](https://www.github.com/googleapis/google-cloud-go/issues/4602)) ([112eea2](https://www.github.com/googleapis/google-cloud-go/commit/112eea20b46bbc34e5f8f65b9812fb3e60107409)), refs [#3626](https://www.github.com/googleapis/google-cloud-go/issues/3626)
* **pubsub:** replace IAMPolicy in API config ([5996846](https://www.github.com/googleapis/google-cloud-go/commit/59968462a3870c6289166fa1161f9b6d9c10e093))

## [1.14.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.13.0...pubsub/v1.14.0) (2021-08-09)


### Features

* **pubsub:** expose CallOptions for pub/sub retries and timeouts ([#4428](https://www.github.com/googleapis/google-cloud-go/issues/4428)) ([8b99dd3](https://www.github.com/googleapis/google-cloud-go/commit/8b99dd356475a750000c06a44fc7b8423d703967))

## [1.13.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.12.2...pubsub/v1.13.0) (2021-07-20)


### Features

* **pubsub/pstest:** add ability to create a pstest server listening on ([#4459](https://www.github.com/googleapis/google-cloud-go/issues/4459)) ([f1b7c8b](https://www.github.com/googleapis/google-cloud-go/commit/f1b7c8b33bc135c6cb8f21cdec586b25d81ea214))

### [1.12.2](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.12.1...pubsub/v1.12.2) (2021-07-08)


### Bug Fixes

* **pubsub:** retry all goaway errors ([#4384](https://www.github.com/googleapis/google-cloud-go/issues/4384)) ([1eae86f](https://www.github.com/googleapis/google-cloud-go/commit/1eae86f1882660d901b9fb0e8dab6f138a048dbb)), refs [#4257](https://www.github.com/googleapis/google-cloud-go/issues/4257)

### [1.12.1](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.12.0...pubsub/v1.12.1) (2021-07-01)


### Bug Fixes

* **pubsub:** retry GOAWAY errors ([#4313](https://www.github.com/googleapis/google-cloud-go/issues/4313)) ([7076fef](https://www.github.com/googleapis/google-cloud-go/commit/7076fef5fef81cce47dbfbab3d7257cc7d3776bc))

## [1.12.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.11.0...pubsub/v1.12.0) (2021-06-23)


### Features

* **pubsub/pstest:** add channel to support user-defined publish responses ([#4251](https://www.github.com/googleapis/google-cloud-go/issues/4251)) ([e1304f4](https://www.github.com/googleapis/google-cloud-go/commit/e1304f435fed4a767f4a652f32f1386979ff794f))


### Bug Fixes

* **pubsub:** fix memory leak issue in publish scheduler ([#4282](https://www.github.com/googleapis/google-cloud-go/issues/4282)) ([22ffc18](https://www.github.com/googleapis/google-cloud-go/commit/22ffc18e522c0f943db57f8c943e7356067bedfd))

## [1.11.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.10.3...pubsub/v1.11.0) (2021-05-27)


### Features

* **pubsub:** add flush method to topic ([#2863](https://www.github.com/googleapis/google-cloud-go/issues/2863)) ([825ddd6](https://www.github.com/googleapis/google-cloud-go/commit/825ddd692363eb2dd8cd253cc5976867e432f547))

### [1.10.3](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.10.2...pubsub/v1.10.3) (2021-04-23)


### Bug Fixes

* **pubsub:** fix failing message storage policy tests ([#4003](https://www.github.com/googleapis/google-cloud-go/issues/4003)) ([8946158](https://www.github.com/googleapis/google-cloud-go/commit/8946158561e1599c164021364e7fcb2a4c4d2f3d))
* **pubsub:** make config call permission error in Receive transparent ([#3985](https://www.github.com/googleapis/google-cloud-go/issues/3985)) ([a1614db](https://www.github.com/googleapis/google-cloud-go/commit/a1614db35a51d21c52bcba5e805071381d8f5133))

### [1.10.2](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.10.1...pubsub/v1.10.2) (2021-04-08)


### Bug Fixes

* **pubsub:** respect subscription message ordering field in scheduler ([#3886](https://www.github.com/googleapis/google-cloud-go/issues/3886)) ([1fcc78a](https://www.github.com/googleapis/google-cloud-go/commit/1fcc78ac6ecb461c3bbede9667436614c9df1535))
* **pubsub:** update quiescenceDur in failing e2e test ([#3780](https://www.github.com/googleapis/google-cloud-go/issues/3780)) ([97e6c69](https://www.github.com/googleapis/google-cloud-go/commit/97e6c696c39bf4cf49fa5ef51145cfcb2a1a5d71))

### [1.10.1](https://www.github.com/googleapis/google-cloud-go/compare/v1.10.0...v1.10.1) (2021-03-04)


### Bug Fixes

* **pubsub:** hide context.Cancelled error in sync pull ([#3752](https://www.github.com/googleapis/google-cloud-go/issues/3752)) ([f88bdc8](https://www.github.com/googleapis/google-cloud-go/commit/f88bdc85072e5ad511a907d98207ebf7d22e9df7))

## [1.10.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.9.1...v1.10.0) (2021-02-10)


### Features

* **pubsub:** add opencensus metrics for outstanding messages/bytes ([#3690](https://www.github.com/googleapis/google-cloud-go/issues/3690)) ([4039b82](https://www.github.com/googleapis/google-cloud-go/commit/4039b82e95b3a8ba2322d1f4fe9e2c21b087a907))

### [1.9.1](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.9.0...v1.9.1) (2020-12-10)

### Bug Fixes

* **pubsub:** fix default stream ack deadline seconds ([#3430](https://www.github.com/googleapis/google-cloud-go/issues/3430)) ([a10263a](https://www.github.com/googleapis/google-cloud-go/commit/a10263adc2ec9483ecedd0bf0b028863342ea760))
* **pubsub:** respect streamAckDeadlineSeconds with MaxExtensionPeriod ([#3367](https://www.github.com/googleapis/google-cloud-go/issues/3367)) ([45131b6](https://www.github.com/googleapis/google-cloud-go/commit/45131b6c526ded2964ffd067c4a5420d508f0b1a))

## [1.9.0](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.8.3...v1.9.0) (2020-12-03)

### Features

- **pubsub:** Enable server side flow control by default with the option to turn it off ([#3154](https://www.github.com/googleapis/google-cloud-go/issues/3154)) ([e392e61](https://www.github.com/googleapis/google-cloud-go/commit/e392e6157ee02a344528de63ab16baba61470b24))

### Refactor

**NOTE**: Several changes were proposed for allowing `Message` and `PublishResult` to be used outside the library. However, the decision was made to only allow packages in `google-cloud-go` to access `NewMessage` and `NewPublishResult` (see #3351).

- **pubsub:** Allow Message and PublishResult to be used outside the package ([#3200](https://www.github.com/googleapis/google-cloud-go/issues/3200)) ([581bf92](https://www.github.com/googleapis/google-cloud-go/commit/581bf92878dcb52ae8ea3633d4b3fcbb7054ff0f))
- **pubsub:** Remove NewMessage and NewPublishResult ([#3232](https://www.github.com/googleapis/google-cloud-go/issues/3232)) ([a781a3a](https://www.github.com/googleapis/google-cloud-go/commit/a781a3ad0c626fc0a7aff0ce33b1ef0830ee2259))

### [1.8.3](https://www.github.com/googleapis/google-cloud-go/compare/pubsub/v1.8.2...v1.8.3) (2020-11-10)

### Bug Fixes

- **pubsub:** retry deadline exceeded errors in Acknowledge ([#3157](https://www.github.com/googleapis/google-cloud-go/issues/3157)) ([ae75b46](https://www.github.com/googleapis/google-cloud-go/commit/ae75b46033d9f14f41c1bde4b9646c93f8e2bbad))

## v1.8.2

- Fixes:
  - fix(pubsub): track errors in published messages opencensus metric (#2970)
  - fix(pubsub): do not propagate context deadline exceeded error (#3055)

## v1.8.1

- Suppress connection is closing on error on subscriber close. (#2951)

## v1.8.0

- Add status code to error injection in pstest. This is a BREAKING CHANGE.

## v1.7.0

- Add reactor options to pstest server. (#2916)

## v1.6.2

- Make message.Modacks thread safe in pstest. (#2755)
- Fix issue with closing publisher and subscriber client errors. (#2867)
- Fix updating subscription filtering/retry policy in pstest. (#2901)

## v1.6.1

- Fix issue where EnableMessageOrdering wasn't being parsed properly to `SubscriptionConfig`.

## v1.6.0

- Fix issue where subscriber streams were limited because it was using a single grpc conn.
  - As a side effect, publisher and subscriber grpc conns are no longer shared.
- Add fake time function in pstest.
- Add support for server side flow control.

## v1.5.0

- Add support for subscription detachment.
- Add support for message filtering in subscriptions.
- Add support for RetryPolicy (server-side feature).
- Fix publish error path when ordering key is disabled.
- Fix panic on Topic.ResumePublish method.

## v1.4.0

- Add support for upcoming ordering keys feature.

## v1.3.1

- Fix bug with removing dead letter policy from a subscription
- Set default value of MaxExtensionPeriod to 0, which is functionally equivalent

## v1.3.0

- Update cloud.google.com/go to v0.54.0

## v1.2.0

- Add support for upcoming dead letter topics feature
- Expose Subscription.ReceiveSettings.MaxExtensionPeriod setting
- Standardize default settings with other client libraries
  - Increase publish delay threshold from 1ms to 10ms
  - Increase subscription MaxExtension from 10m to 60m
- Always send keepalive/heartbeat ping on StreamingPull streams to minimize
  stream reopen requests

## v1.1.0

- Limit default grpc connections to 4.
- Fix issues with OpenCensus metric for pull count not including synchronous pull messages.
- Fix issue with publish bundle size calculations.
- Add ClearMessages method to pstest server.

## v1.0.1

Small fix to a package name.

## v1.0.0

This is the first tag to carve out pubsub as its own module. See:
https://github.com/golang/go/wiki/Modules#is-it-possible-to-add-a-module-to-a-multi-module-repository.
