# Changes

## [1.18.1](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.18.0...bigtable/v1.18.1) (2022-12-02)


### Bug Fixes

* **bigtable:** downgrade some dependencies ([7540152](https://github.com/googleapis/google-cloud-go/commit/754015236d5af7c82a75da218b71a87b9ead6eb5))

## [1.18.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.17.0...bigtable/v1.18.0) (2022-11-10)


### Features

* **bigtable:** Add support for request stats ([#6991](https://github.com/googleapis/google-cloud-go/issues/6991)) ([609421e](https://github.com/googleapis/google-cloud-go/commit/609421e87ff25971f3fc29e15dbcdaa7fba02d11))

## [1.17.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.16.0...bigtable/v1.17.0) (2022-11-03)


### Features

* **bigtable:** Add create table metadata support ([#6813](https://github.com/googleapis/google-cloud-go/issues/6813)) ([d497377](https://github.com/googleapis/google-cloud-go/commit/d4973774b6b31a2091bcff06c01af6acf4378e93))
* **bigtable:** Add update table metadata support ([#6746](https://github.com/googleapis/google-cloud-go/issues/6746)) ([f19ffad](https://github.com/googleapis/google-cloud-go/commit/f19ffada53d45919e872bec7089f0a540a35755d))
* **bigtable:** Update genproto ([#6710](https://github.com/googleapis/google-cloud-go/issues/6710)) ([34f3aa4](https://github.com/googleapis/google-cloud-go/commit/34f3aa4c36c9a082e4bde1aad6f18951eb48cb51))


### Bug Fixes

* **bigtable:** CellsPer(Row|Column)LimitFilter should error with arguments &lt;= 0. ([#6495](https://github.com/googleapis/google-cloud-go/issues/6495)) ([7724d8f](https://github.com/googleapis/google-cloud-go/commit/7724d8f077db62d543571b11bd17d5494fbd0260))
* **bigtable:** Fix flaky AdminBackUp test ([#6917](https://github.com/googleapis/google-cloud-go/issues/6917)) ([45cc61e](https://github.com/googleapis/google-cloud-go/commit/45cc61ecad8dd67ac1b17b1f8e03043ff6ab4792))

## [1.16.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.15.0...bigtable/v1.16.0) (2022-07-27)


### Features

* **bigtable:** add PolicyType for GCPolicy and expose public methods for different types of GC policies ([#6314](https://github.com/googleapis/google-cloud-go/issues/6314)) ([2971037](https://github.com/googleapis/google-cloud-go/commit/2971037040dd5c2cf712e33ef49cfdfc238c02cc))
* **bigtable:** adds autoscaling target storage per node ([#6317](https://github.com/googleapis/google-cloud-go/issues/6317)) ([5eab4c3](https://github.com/googleapis/google-cloud-go/commit/5eab4c336075ae5aae78794d73bd0d8d1342813c))


### Bug Fixes

* **bigtable:** make code buildable ([#6436](https://github.com/googleapis/google-cloud-go/issues/6436)) ([6bd5ce8](https://github.com/googleapis/google-cloud-go/commit/6bd5ce85ba52fff676bdef2a2bb7fc8ed001e766)), refs [#6419](https://github.com/googleapis/google-cloud-go/issues/6419)

## [1.15.0](https://github.com/googleapis/google-cloud-go/compare/bigtable-v1.14.0...bigtable/v1.15.0) (2022-07-07)


### Features

* **bigtable:** add file for tracking version ([17b36ea](https://github.com/googleapis/google-cloud-go/commit/17b36ead42a96b1a01105122074e65164357519e))
* **bigtable:** add GC policy to FamilyInfo. ([#6234](https://github.com/googleapis/google-cloud-go/issues/6234)) ([eb0540d](https://github.com/googleapis/google-cloud-go/commit/eb0540d6f6bbc28074195730178991718c9c0d83))
* **bigtable:** loadtest support app profile ([#5882](https://github.com/googleapis/google-cloud-go/issues/5882)) ([ec00e5a](https://github.com/googleapis/google-cloud-go/commit/ec00e5a3f0ab0e59bbdb6915ffb53a9dca5f168e))
* **bigtable:** support PingAndWarm in emulator ([#5803](https://github.com/googleapis/google-cloud-go/issues/5803)) ([9b943d5](https://github.com/googleapis/google-cloud-go/commit/9b943d59fe7e86a037d239663dc64901e9b88a62))


### Bug Fixes

* **bigtable:** use internal.Version that is auto-updated for UA ([#5679](https://github.com/googleapis/google-cloud-go/issues/5679)) ([bd2c600](https://github.com/googleapis/google-cloud-go/commit/bd2c600145b1fd12c3ef4f314e4d72543e575206)), refs [#3330](https://github.com/googleapis/google-cloud-go/issues/3330)

## [1.14.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.13.0...bigtable/v1.14.0) (2022-05-26)


### Features

* **bigtable:** add file for tracking version ([17b36ea](https://github.com/googleapis/google-cloud-go/commit/17b36ead42a96b1a01105122074e65164357519e))
* **bigtable:** loadtest support app profile ([#5882](https://github.com/googleapis/google-cloud-go/issues/5882)) ([ec00e5a](https://github.com/googleapis/google-cloud-go/commit/ec00e5a3f0ab0e59bbdb6915ffb53a9dca5f168e))
* **bigtable:** support PingAndWarm in emulator ([#5803](https://github.com/googleapis/google-cloud-go/issues/5803)) ([9b943d5](https://github.com/googleapis/google-cloud-go/commit/9b943d59fe7e86a037d239663dc64901e9b88a62))


### Bug Fixes

* **bigtable:** use internal.Version that is auto-updated for UA ([#5679](https://github.com/googleapis/google-cloud-go/issues/5679)) ([bd2c600](https://github.com/googleapis/google-cloud-go/commit/bd2c600145b1fd12c3ef4f314e4d72543e575206)), refs [#3330](https://github.com/googleapis/google-cloud-go/issues/3330)

## [1.13.0](https://www.github.com/googleapis/google-cloud-go/compare/bigtable/v1.12.0...bigtable/v1.13.0) (2022-01-24)


### Features

* **bigtable/spanner:** add google-c2p dependence to bigtable and spanner ([#5090](https://www.github.com/googleapis/google-cloud-go/issues/5090)) ([5343756](https://www.github.com/googleapis/google-cloud-go/commit/534375668b5b81bae5ef750c96856bef027f9d1e))
* **bigtable:** add google-c2p dependence ([5343756](https://www.github.com/googleapis/google-cloud-go/commit/534375668b5b81bae5ef750c96856bef027f9d1e))
* **bigtable:** add support for autoscaling ([#5232](https://www.github.com/googleapis/google-cloud-go/issues/5232)) ([a59d1ac](https://www.github.com/googleapis/google-cloud-go/commit/a59d1ac080c71446a3d8821e83c8fc8b54b1c4f0))

## [1.12.0](https://www.github.com/googleapis/google-cloud-go/compare/bigtable/v1.11.0...bigtable/v1.12.0) (2021-11-15)


### Features

* **bigtable/cbt:** cbt 'import' cmd to parse a .csv file and write to CBT ([#5072](https://www.github.com/googleapis/google-cloud-go/issues/5072)) ([5a2ed6b](https://www.github.com/googleapis/google-cloud-go/commit/5a2ed6b2cd1c304e0f59daa29959863bff9b5c29))

## [1.11.0](https://www.github.com/googleapis/google-cloud-go/compare/bigtable/v1.10.1...bigtable/v1.11.0) (2021-10-29)


### Features

* **bigtable/cmd/cbt:** Add a timeout option ([#4276](https://www.github.com/googleapis/google-cloud-go/issues/4276)) ([ae8a9a1](https://www.github.com/googleapis/google-cloud-go/commit/ae8a9a103f380917ab2c8f7ccaddbe5f40670a7a))


### Bug Fixes

* **bigtable/bttest:** Cells per row offset filters didn't implement truthiness correctly, breaking conditional filters ([#4287](https://www.github.com/googleapis/google-cloud-go/issues/4287)) ([a1a2a77](https://www.github.com/googleapis/google-cloud-go/commit/a1a2a77f33fa27eb78f1ddcbe8c78c2444f638eb))
* **bigtable/bttest:** Emulator too lenient for empty RowMutation ([#4359](https://www.github.com/googleapis/google-cloud-go/issues/4359)) ([35ceae2](https://www.github.com/googleapis/google-cloud-go/commit/35ceae2ce75bf7dfde4ccfe57de246c7adec83e0))
* **bigtable/bttest:** emulator too lenient regarding RowFilter and CheckAndMutateRow conditions ([#4095](https://www.github.com/googleapis/google-cloud-go/issues/4095)) ([99537fe](https://www.github.com/googleapis/google-cloud-go/commit/99537fef402a683d481bca7688d6e0c3b536b26b))
* **bigtable/bttest:** fix ModifyColumnFamilies to purge data ([#4096](https://www.github.com/googleapis/google-cloud-go/issues/4096)) ([2095028](https://www.github.com/googleapis/google-cloud-go/commit/2095028bb83edddddefa52ce4bb343ed1744b91c))
* **bigtable:** emulator crashes in SampleRowKeys ([#4455](https://www.github.com/googleapis/google-cloud-go/issues/4455)) ([691e923](https://www.github.com/googleapis/google-cloud-go/commit/691e923fca9bd3194ff4ba49bd2d899518875d7c))
* **bigtable:** fix [#4338](https://www.github.com/googleapis/google-cloud-go/issues/4338) by removing obsolete with block ([#4353](https://www.github.com/googleapis/google-cloud-go/issues/4353)) ([1cf34b3](https://www.github.com/googleapis/google-cloud-go/commit/1cf34b35e69127a57ab90be583c974a2467b3a97))

### [1.10.1](https://www.github.com/googleapis/google-cloud-go/compare/bigtable/v1.10.0...bigtable/v1.10.1) (2021-06-02)


### Bug Fixes

* **bigtable:** Guard for nil EncryptionConfig in Clusters, GetCluster ([#4113](https://www.github.com/googleapis/google-cloud-go/issues/4113)) ([a17ff67](https://www.github.com/googleapis/google-cloud-go/commit/a17ff67164645328d301ee1884c7ba42f35ef7ba))

## [1.10.0](https://www.github.com/googleapis/google-cloud-go/compare/bigtable/v1.9.0...bigtable/v1.10.0) (2021-05-03)


### Features

* **bigtable:** allow restore backup to different instance ([#3489](https://www.github.com/googleapis/google-cloud-go/issues/3489)) ([#4014](https://www.github.com/googleapis/google-cloud-go/issues/4014)) ([b08b265](https://www.github.com/googleapis/google-cloud-go/commit/b08b2651bca6920ef4c25d11d0b808e40a979835))

## [1.9.0](https://www.github.com/googleapis/google-cloud-go/compare/bigtable/v1.8.0...bigtable/v1.9.0) (2021-04-30)


### Features

* **bigtable:** Customer Managed Encryption (CMEK) ([#3899](https://www.github.com/googleapis/google-cloud-go/issues/3899)) ([e9684ab](https://www.github.com/googleapis/google-cloud-go/commit/e9684ab1e8db6a148c72fc277f61dcfb0cd351b7))

## [1.8.0](https://www.github.com/googleapis/google-cloud-go/compare/v1.7.1...v1.8.0) (2021-02-24)


### Features

* **bigtable:** support partial results in InstanceAdminClient.Clusters() ([#2932](https://www.github.com/googleapis/google-cloud-go/issues/2932)) ([28decb5](https://www.github.com/googleapis/google-cloud-go/commit/28decb55c366c5ec67e04800aa06179943b765f6))

### [1.7.1](https://www.github.com/googleapis/google-cloud-go/compare/bigtable/v1.7.0...v1.7.1) (2021-01-25)


### Bug Fixes

* **bigtable:** replace unsafe exec in cbt ([#3591](https://www.github.com/googleapis/google-cloud-go/issues/3591)) ([7c1b0c2](https://www.github.com/googleapis/google-cloud-go/commit/7c1b0c2deb737e696a72bd44bc610223d62b7d0e))

## [1.7.0](https://www.github.com/googleapis/google-cloud-go/compare/bigtable/v1.6.0...v1.7.0) (2021-01-19)


### Features

* **bigtable:** Add a DirectPath fallback integration test ([#3384](https://www.github.com/googleapis/google-cloud-go/issues/3384)) ([e6684c3](https://www.github.com/googleapis/google-cloud-go/commit/e6684c39599221e9a1e22a790305e42e8ce5d903))
* **bigtable:** attempt DirectPath by default ([#3558](https://www.github.com/googleapis/google-cloud-go/issues/3558)) ([330a3f4](https://www.github.com/googleapis/google-cloud-go/commit/330a3f489e3c534f647549be11f342997243ec3b))
* **bigtable:** Backup Level IAM ([#3222](https://www.github.com/googleapis/google-cloud-go/issues/3222)) ([c77c822](https://www.github.com/googleapis/google-cloud-go/commit/c77c822b5aadb0f5f3ae9381acafdee496047f8a))
* **bigtable:** run E2E test over DirectPath ([#3116](https://www.github.com/googleapis/google-cloud-go/issues/3116)) ([948452c](https://www.github.com/googleapis/google-cloud-go/commit/948452ce896d3f44c0e22cdaf69e122f26a3c912))

## v1.6.0
- Add support partial results in InstanceAdminClient.Instances. In the case of
  partial availability, available instances will be returned along with an
  ErrPartiallyUnavailable error.
- Add support for label filters.
- Fix max valid timestamp in the emulator to allow reversed timestamp support.

## v1.5.0
- Add support for managed backups.

## v1.4.0
- Add support for instance state and labels to the admin API.
- Add metadata header to all data requests.
- Fix bug in timestamp to time conversion.

## v1.3.0

- Clients now use transport/grpc.DialPool rather than Dial.
  - Connection pooling now does not use the deprecated (and soon to be removed) gRPC load balancer API.

## v1.2.0

- Update cbt usage string.

- Fix typo in cbt tool.

- Ignore empty lines in cbtrc.

- Emulator now rejects microseconds precision.

## v1.1.0

- Add support to cbt tool to drop all rows from a table.

- Adds a method to update an instance with clusters.

- Adds StorageType to ClusterInfo.

- Add support for the `-auth-token` flag to cbt tool.

- Adds support for Table-level IAM, including some bug fixes.

## v1.0.0

This is the first tag to carve out bigtable as its own module. See:
https://github.com/golang/go/wiki/Modules#is-it-possible-to-add-a-module-to-a-multi-module-repository.
