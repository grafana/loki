# Changes

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
