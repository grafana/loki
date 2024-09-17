# Changes

## [1.29.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.28.0...bigtable/v1.29.0) (2024-08-09)


### Features

* **bigtable/admin:** Add fields and the BackupType proto for Hot Backups ([649c075](https://github.com/googleapis/google-cloud-go/commit/649c075d5310e2fac64a0b65ec445e7caef42cb0))
* **bigtable:** Remove deprecated Bytes from BigEndianBytesEncoding ([#10659](https://github.com/googleapis/google-cloud-go/issues/10659)) ([0bb1a6d](https://github.com/googleapis/google-cloud-go/commit/0bb1a6de1fba3307cc770e2bcaebe93c8fe9d628))


### Bug Fixes

* **bigtable:** Update google.golang.org/api to v0.191.0 ([5b32644](https://github.com/googleapis/google-cloud-go/commit/5b32644eb82eb6bd6021f80b4fad471c60fb9d73))


### Documentation

* **bigtable/admin:** Clarify comments and fix typos ([649c075](https://github.com/googleapis/google-cloud-go/commit/649c075d5310e2fac64a0b65ec445e7caef42cb0))

## [1.28.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.27.1...bigtable/v1.28.0) (2024-08-03)


### Features

* **bigtable:** Add MergeToCell support to the bigtable emulator and client ([#10366](https://github.com/googleapis/google-cloud-go/issues/10366)) ([0211c95](https://github.com/googleapis/google-cloud-go/commit/0211c95e0404aad31be5bec6d5855f0bc5358161))
* **bigtable:** Add support for new functions ([#10582](https://github.com/googleapis/google-cloud-go/issues/10582)) ([a49ab59](https://github.com/googleapis/google-cloud-go/commit/a49ab593a7495c2cfff106594762b9a6c79eb8b2))
* **bigtable:** Expose protoToType ([#10602](https://github.com/googleapis/google-cloud-go/issues/10602)) ([643a8e3](https://github.com/googleapis/google-cloud-go/commit/643a8e356632160c143e94f905c72b4e6452f5a6))


### Bug Fixes

* **bigtable/emulator:** Sending empty row in SampleRowKeys response ([#10611](https://github.com/googleapis/google-cloud-go/issues/10611)) ([928f1a7](https://github.com/googleapis/google-cloud-go/commit/928f1a77191fbf4736051305e0ad67b69bae11fb))
* **bigtable:** Move usage to local proto definitions ([#10598](https://github.com/googleapis/google-cloud-go/issues/10598)) ([ce31365](https://github.com/googleapis/google-cloud-go/commit/ce31365acc54fdf0970fc9552b1758c8fef4762f))

## [1.27.1](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.27.0...bigtable/v1.27.1) (2024-07-25)


### Bug Fixes

* **bigtable:** Start generating proto sources ([5b4b0f7](https://github.com/googleapis/google-cloud-go/commit/5b4b0f7878276ab5709011778b1b4a6ffd30a60b))

## [1.27.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.26.0...bigtable/v1.27.0) (2024-07-25)


### Features

* **bigtable:** Built-in client side metrics ([#10046](https://github.com/googleapis/google-cloud-go/issues/10046)) ([a747f0a](https://github.com/googleapis/google-cloud-go/commit/a747f0a49b79c0fe3034f7374b47ca56fc5ce0f5))

## [1.26.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.25.0...bigtable/v1.26.0) (2024-07-23)


### Features

* **bigtable/emulator:** Allow listening on Unix Domain Sockets ([#9665](https://github.com/googleapis/google-cloud-go/issues/9665)) ([424494c](https://github.com/googleapis/google-cloud-go/commit/424494ce23db13468a4ea3e3be6ed1dee028ecdb))
* **bigtable:** Add column family type to FamilyInfo in TableInfo ([#10520](https://github.com/googleapis/google-cloud-go/issues/10520)) ([fd16a17](https://github.com/googleapis/google-cloud-go/commit/fd16a1785df6f1378aecb3cd6a7f2c9bcc40c6c7))
* **bigtable:** Mark CBT Authorized View admin APIs as unimplemented in the emulator  ([#10562](https://github.com/googleapis/google-cloud-go/issues/10562)) ([6b32871](https://github.com/googleapis/google-cloud-go/commit/6b328715c83c8fa2bfd1c3b6b64acd8f1bd486f2))


### Bug Fixes

* **bigtable:** Add quotes to end of range ([#10488](https://github.com/googleapis/google-cloud-go/issues/10488)) ([142b153](https://github.com/googleapis/google-cloud-go/commit/142b15384d4d818faf30f3bae4567c7f579f4079))
* **bigtable:** Bump google.golang.org/api@v0.187.0 ([8fa9e39](https://github.com/googleapis/google-cloud-go/commit/8fa9e398e512fd8533fd49060371e61b5725a85b))
* **bigtable:** Bump google.golang.org/grpc@v1.64.1 ([8ecc4e9](https://github.com/googleapis/google-cloud-go/commit/8ecc4e9622e5bbe9b90384d5848ab816027226c5))
* **bigtable:** Update dependencies ([257c40b](https://github.com/googleapis/google-cloud-go/commit/257c40bd6d7e59730017cf32bda8823d7a232758))

## [1.25.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.24.0...bigtable/v1.25.0) (2024-06-20)


### Features

* **bigtable:** Add string type to supported Bigtable type ([#10306](https://github.com/googleapis/google-cloud-go/issues/10306)) ([18fa7e4](https://github.com/googleapis/google-cloud-go/commit/18fa7e4961d055939078833d0442a415fac96ae6))

## [1.24.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.23.0...bigtable/v1.24.0) (2024-05-28)


### Features

* **bigtable:** Add ignore_warnings flag to SetGcPolicy ([#9372](https://github.com/googleapis/google-cloud-go/issues/9372)) ([0e6413d](https://github.com/googleapis/google-cloud-go/commit/0e6413db32f2a27269602fa88afa762abdb837c0))
* **bigtable:** Adding automated backups ([#9702](https://github.com/googleapis/google-cloud-go/issues/9702)) ([9738386](https://github.com/googleapis/google-cloud-go/commit/9738386b1b0cf7e490e1bdc0b16d791d5e88b249))


### Bug Fixes

* **bigtable/bttest:** Error when applying a mutation with an empty row key ([#9512](https://github.com/googleapis/google-cloud-go/issues/9512)) ([7231423](https://github.com/googleapis/google-cloud-go/commit/723142310912d729b20138d9af40d2aeea826838))
* **bigtable:** Reject misspecified Automated Backup Policies when updating a table ([#10226](https://github.com/googleapis/google-cloud-go/issues/10226)) ([84e45ce](https://github.com/googleapis/google-cloud-go/commit/84e45cee7eef2576c6832e658d59ffa749688fbd))
* **bigtable:** Retry on RST_STREAM error ([#9673](https://github.com/googleapis/google-cloud-go/issues/9673)) ([d4da4a5](https://github.com/googleapis/google-cloud-go/commit/d4da4a5a4a5838f0f4edaf3fe9c6d1fde355f782))

## [1.23.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.22.0...bigtable/v1.23.0) (2024-04-29)


### Features

* **bigtable/spanner:** Remove grpclb ([#9186](https://github.com/googleapis/google-cloud-go/issues/9186)) ([480f9a0](https://github.com/googleapis/google-cloud-go/commit/480f9a0ea8e159299dd3f909e2c0d8b5e771c580))
* **bigtable:** Allow non-default service account in DirectPath ([#9040](https://github.com/googleapis/google-cloud-go/issues/9040)) ([c2df09c](https://github.com/googleapis/google-cloud-go/commit/c2df09c32808e7dab35ca5084e80e0b9c6c0e6f8))
* **bigtable:** Support AuthorizedView in data and admin client ([#9515](https://github.com/googleapis/google-cloud-go/issues/9515)) ([8259645](https://github.com/googleapis/google-cloud-go/commit/8259645be0d9e635a41944788f2e65d2b52c4dbb))


### Bug Fixes

* **bigtable:** Accept nil RowSet to read all rows ([#9327](https://github.com/googleapis/google-cloud-go/issues/9327)) ([cd36506](https://github.com/googleapis/google-cloud-go/commit/cd36506d377d2d5199402a58360c23ba4ce9a3d4))
* **bigtable:** Add internaloption.WithDefaultEndpointTemplate ([3b41408](https://github.com/googleapis/google-cloud-go/commit/3b414084450a5764a0248756e95e13383a645f90))
* **bigtable:** Bump x/net to v0.24.0 ([ba31ed5](https://github.com/googleapis/google-cloud-go/commit/ba31ed5fda2c9664f2e1cf972469295e63deb5b4))
* **bigtable:** Resolve DeadlineExceeded conformance test failures ([#9688](https://github.com/googleapis/google-cloud-go/issues/9688)) ([54d2990](https://github.com/googleapis/google-cloud-go/commit/54d2990c5cd66e274279a1534844e1c4018dd5f5))
* **bigtable:** Update protobuf dep to v1.33.0 ([30b038d](https://github.com/googleapis/google-cloud-go/commit/30b038d8cac0b8cd5dd4761c87f3f298760dd33a))

## [1.22.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.21.0...bigtable/v1.22.0) (2024-03-11)


### Features

* **bigtable:** Add aggregate support to the bigtable emulator and client ([c250928](https://github.com/googleapis/google-cloud-go/commit/c25092892dfc55c86784f77222d2bd96e40bd2d1))


### Bug Fixes

* **bigtable/bttest:** Make table gc release memory ([#3930](https://github.com/googleapis/google-cloud-go/issues/3930)) ([7d6ff39](https://github.com/googleapis/google-cloud-go/commit/7d6ff39309de41d8847d27241ebee41a30cf5aa7))
* **bigtable:** Allow micro seconds in filter in Bigtable emulator ([#9414](https://github.com/googleapis/google-cloud-go/issues/9414)) ([9fe6061](https://github.com/googleapis/google-cloud-go/commit/9fe60618e4b0004d23920c8c5aa25dc38c6cf68a))
* **bigtable:** Fix deadline exceeded conformance test ([#9220](https://github.com/googleapis/google-cloud-go/issues/9220)) ([092ee0b](https://github.com/googleapis/google-cloud-go/commit/092ee0ba59267b8fb4d3f4e7727ed3ccbf81e7e7))


### Miscellaneous Chores

* **bigtable:** Release 1.22.0 ([#9547](https://github.com/googleapis/google-cloud-go/issues/9547)) ([48614ab](https://github.com/googleapis/google-cloud-go/commit/48614ab80d8b3e1da876832c88912007ad0e008b))

## [1.21.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.20.0...bigtable/v1.21.0) (2023-12-04)


### Features

* **bigtable:** Add support for reverse scans ([#8755](https://github.com/googleapis/google-cloud-go/issues/8755)) ([244d135](https://github.com/googleapis/google-cloud-go/commit/244d1357cb1b6ce3b971d367693f6cb6090018d4))
* **bigtable:** Support copy backup in admin client ([#9005](https://github.com/googleapis/google-cloud-go/issues/9005)) ([834c47f](https://github.com/googleapis/google-cloud-go/commit/834c47fb3bd9e8a21082325780b2dcfd4c6d52c6))


### Bug Fixes

* **bigtable:** Bump google.golang.org/api to v0.149.0 ([8d2ab9f](https://github.com/googleapis/google-cloud-go/commit/8d2ab9f320a86c1c0fab90513fc05861561d0880))
* **bigtable:** Return cluster error for Update when populated ([#8657](https://github.com/googleapis/google-cloud-go/issues/8657)) ([2105434](https://github.com/googleapis/google-cloud-go/commit/2105434f27a16ac05790c40b74d3a251ec584527))
* **bigtable:** Update golang.org/x/net to v0.17.0 ([174da47](https://github.com/googleapis/google-cloud-go/commit/174da47254fefb12921bbfc65b7829a453af6f5d))
* **bigtable:** Update grpc-go to v1.56.3 ([343cea8](https://github.com/googleapis/google-cloud-go/commit/343cea8c43b1e31ae21ad50ad31d3b0b60143f8c))
* **bigtable:** Update grpc-go to v1.59.0 ([81a97b0](https://github.com/googleapis/google-cloud-go/commit/81a97b06cb28b25432e4ece595c55a9857e960b7))

## [1.20.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.19.0...bigtable/v1.20.0) (2023-10-02)


### Features

* **bigtable/bttest:** Support reverse scans ([#8198](https://github.com/googleapis/google-cloud-go/issues/8198)) ([b8f164f](https://github.com/googleapis/google-cloud-go/commit/b8f164fcdf2be3a6fcf7918b3703e224801cc513))
* **bigtable:** Support last_scanned_row in the bigtable client ([#8345](https://github.com/googleapis/google-cloud-go/issues/8345)) ([961dd38](https://github.com/googleapis/google-cloud-go/commit/961dd38f9e461d487ba8b6ee26ea14d872991eaf))
* **bigtable:** Support last_scanned_row_key in emulator ([#8343](https://github.com/googleapis/google-cloud-go/issues/8343)) ([d53ef45](https://github.com/googleapis/google-cloud-go/commit/d53ef459893b29e7050f943da00bcd0a3f3ff900))


### Bug Fixes

* **bigtable:** Add missing veneer header ([#8607](https://github.com/googleapis/google-cloud-go/issues/8607)) ([b56f557](https://github.com/googleapis/google-cloud-go/commit/b56f557ff713d70025d2ee0e0acc2169fda77c77))

## [1.19.0](https://github.com/googleapis/google-cloud-go/compare/bigtable/v1.18.1...bigtable/v1.19.0) (2023-07-06)


### Features

* **bigtable:** Add change stream config to create and update table ([#8180](https://github.com/googleapis/google-cloud-go/issues/8180)) ([32897ce](https://github.com/googleapis/google-cloud-go/commit/32897cec9be7413fa09b403199980e782ae52107))
* **bigtable:** Update all direct dependencies ([b340d03](https://github.com/googleapis/google-cloud-go/commit/b340d030f2b52a4ce48846ce63984b28583abde6))
* **bigtable:** Update iam and longrunning deps ([91a1f78](https://github.com/googleapis/google-cloud-go/commit/91a1f784a109da70f63b96414bba8a9b4254cddd))


### Bug Fixes

* **bigtable:** REST query UpdateMask bug ([df52820](https://github.com/googleapis/google-cloud-go/commit/df52820b0e7721954809a8aa8700b93c5662dc9b))
* **bigtable:** Update grpc to v1.55.0 ([1147ce0](https://github.com/googleapis/google-cloud-go/commit/1147ce02a990276ca4f8ab7a1ab65c14da4450ef))
* **bigtable:** Use fieldmask directly instead of field_mask genproto alias ([#8032](https://github.com/googleapis/google-cloud-go/issues/8032)) ([cae6cd6](https://github.com/googleapis/google-cloud-go/commit/cae6cd6d0e09e98157879fb03fb23f718f4d2bb3))

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
