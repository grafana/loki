# Release History

## 0.5.1 (2022-10-11)

### Bugs Fixed

* `GetSASURL()`: for container and blob clients, don't add a forward slash before the query string
* Fixed issue [#19249](https://github.com/Azure/azure-sdk-for-go/issues/19249) by increasing service version to '2020-02-10'.

### Other Changes

* Improved docs for client constructors.
* Updating azcore version to 1.1.4

## 0.5.0 (2022-09-29)

### Breaking Changes

* Complete architectural change for better user experience. Please view the [README](https://github.com/Azure/azure-sdk-for-go/tree/main/sdk/storage/azblob#readme)

### Features Added

* Added [UserDelegationCredential](https://learn.microsoft.com/rest/api/storageservices/create-user-delegation-sas) which resolves [#18976](https://github.com/Azure/azure-sdk-for-go/issues/18976), [#16916](https://github.com/Azure/azure-sdk-for-go/issues/16916), [#18977](https://github.com/Azure/azure-sdk-for-go/issues/18977)
* Added [Restore Container API](https://learn.microsoft.com/rest/api/storageservices/restore-container).

### Bugs Fixed

* Fixed issue [#18767](https://github.com/Azure/azure-sdk-for-go/issues/18767)
* Fix deadlock when error writes are slow [#16937](https://github.com/Azure/azure-sdk-for-go/pull/16937)

## 0.4.1 (2022-05-12)

### Other Changes

* Updated to latest `azcore` and `internal` modules

## 0.4.0 (2022-04-19)

### Breaking Changes

* Fixed Issue #17150 : Renaming/refactoring high level methods.
* Fixed Issue #16972 : Constructors should return clients by reference.
* Renaming the options bags to match the naming convention same as that of response. The behaviour of options bags
  remains the same.

### Bugs Fixed

* Fixed Issue #17515 : SetTags options bag missing leaseID.
* Fixed Issue #17423 : Drop "Type" suffix from `GeoReplicationStatusType`.
* Fixed Issue #17335 : Nil pointer exception when passing nil options bag in `ListBlobsFlat` API call.
* Fixed Issue #17188 : `BlobURLParts` not supporting VersionID
* Fixed Issue #17152 , Issue #17131 , Issue #17061 : `UploadStreamToBlockBlob` / `UploadStreamToBlockBlob` methods
  ignoring the options bag.
* Fixed Issue #16920 : Fixing error handling example.
* Fixed Issue #16786 : Refactoring of autorest code generation definition and adding necessary transformations.
* Fixed Issue #16679 : Response parsing issue in List blobs API.

## 0.3.0 (2022-02-09)

### Breaking Changes

* Updated to latest `azcore`. Public surface area is unchanged.
* [#16978](https://github.com/Azure/azure-sdk-for-go/pull/16978): The `DownloadResponse.Body` parameter is
  now `*RetryReaderOptions`.

### Bugs Fixed

* Fixed Issue #16193 : `azblob.GetSASToken` wrong signed resource.
* Fixed Issue #16223 : `HttpRange` does not expose its fields.
* Fixed Issue #16254 : Issue passing reader to upload `BlockBlobClient`
* Fixed Issue #16295 : Problem with listing blobs by using of `ListBlobsHierarchy()`
* Fixed Issue #16542 : Empty `StorageError` in the Azurite environment
* Fixed Issue #16679 : Unable to access Metadata when listing blobs
* Fixed Issue #16816 : `ContainerClient.GetSASToken` doesn't allow list permission.
* Fixed Issue #16988 : Too many arguments in call to `runtime.NewResponseError`

## 0.2.0 (2021-11-03)

### Breaking Changes

* Clients now have one constructor per authentication method

## 0.1.0 (2021-09-13)

### Features Added

* This is the initial preview release of the `azblob` library
