# Code Generation - Azure Blob SDK for Golang

### Settings

```yaml
go: true
clear-output-folder: false
version: "^3.0.0"
license-header: MICROSOFT_MIT_NO_VERSION
input-file: "https://raw.githubusercontent.com/Azure/azure-rest-api-specs/e515b6251fdc21015282d2e84b85beec7c091763/specification/storage/data-plane/Microsoft.BlobStorage/preview/2020-10-02/blob.json"
credential-scope: "https://storage.azure.com/.default"
output-folder: .
file-prefix: "zz_"
openapi-type: "data-plane"
verbose: true
security: AzureKey
modelerfour:
  group-parameters: false
  seal-single-value-enum-by-default: true
  lenient-model-deduplication: true
export-clients: true
use: "@autorest/go@4.0.0-preview.43"
```

### Remove pager methods and export various generated methods in container client

``` yaml
directive:
  - from: zz_container_client.go
    where: $
    transform: >-
      return $.
        replace(/func \(client \*ContainerClient\) NewListBlobFlatSegmentPager\(.+\/\/ listBlobFlatSegmentCreateRequest creates the ListBlobFlatSegment request/s, `// listBlobFlatSegmentCreateRequest creates the ListBlobFlatSegment request`).
        replace(/\(client \*ContainerClient\) listBlobFlatSegmentCreateRequest\(/, `(client *ContainerClient) ListBlobFlatSegmentCreateRequest(`).
        replace(/\(client \*ContainerClient\) listBlobFlatSegmentHandleResponse\(/, `(client *ContainerClient) ListBlobFlatSegmentHandleResponse(`);
```

### Remove pager methods and export various generated methods in service client

``` yaml
directive:
  - from: zz_service_client.go
    where: $
    transform: >-
      return $.
        replace(/func \(client \*ServiceClient\) NewListContainersSegmentPager\(.+\/\/ listContainersSegmentCreateRequest creates the ListContainersSegment request/s, `// listContainersSegmentCreateRequest creates the ListContainersSegment request`).
        replace(/\(client \*ServiceClient\) listContainersSegmentCreateRequest\(/, `(client *ServiceClient) ListContainersSegmentCreateRequest(`).
        replace(/\(client \*ServiceClient\) listContainersSegmentHandleResponse\(/, `(client *ServiceClient) ListContainersSegmentHandleResponse(`);
```

### Fix BlobMetadata.

``` yaml
directive:
- from: swagger-document
  where: $.definitions
  transform: >
    delete $.BlobMetadata["properties"];

```

### Don't include container name or blob in path - we have direct URIs.

``` yaml
directive:
- from: swagger-document
  where: $["x-ms-paths"]
  transform: >
    for (const property in $)
    {
        if (property.includes('/{containerName}/{blob}'))
        {
            $[property]["parameters"] = $[property]["parameters"].filter(function(param) { return (typeof param['$ref'] === "undefined") || (false == param['$ref'].endsWith("#/parameters/ContainerName") && false == param['$ref'].endsWith("#/parameters/Blob"))});
        } 
        else if (property.includes('/{containerName}'))
        {
            $[property]["parameters"] = $[property]["parameters"].filter(function(param) { return (typeof param['$ref'] === "undefined") || (false == param['$ref'].endsWith("#/parameters/ContainerName"))});
        }
    }
```

### Remove DataLake stuff.

``` yaml
directive:
- from: swagger-document
  where: $["x-ms-paths"]
  transform: >
    for (const property in $)
    {
        if (property.includes('filesystem'))
        {
            delete $[property];
        }
    }
```

### Remove DataLakeStorageError

``` yaml
directive:
- from: swagger-document
  where: $.definitions
  transform: >
    delete $.DataLakeStorageError;
```

### Fix 304s

``` yaml
directive:
- from: swagger-document
  where: $["x-ms-paths"]["/{containerName}/{blob}"]
  transform: >
    $.get.responses["304"] = {
      "description": "The condition specified using HTTP conditional header(s) is not met.",
      "x-az-response-name": "ConditionNotMetError",
      "headers": { "x-ms-error-code": { "x-ms-client-name": "ErrorCode", "type": "string" } }
    };
```

### Fix GeoReplication

``` yaml
directive:
- from: swagger-document
  where: $.definitions
  transform: >
    delete $.GeoReplication.properties.Status["x-ms-enum"];
    $.GeoReplication.properties.Status["x-ms-enum"] = {
        "name": "BlobGeoReplicationStatus",
        "modelAsString": false
    };
```

### Fix RehydratePriority

``` yaml
directive:
- from: swagger-document
  where: $.definitions
  transform: >
    delete $.RehydratePriority["x-ms-enum"];
    $.RehydratePriority["x-ms-enum"] = {
        "name": "RehydratePriority",
        "modelAsString": false
    };
```

### Fix BlobDeleteType

``` yaml
directive:
- from: swagger-document
  where: $.parameters
  transform: >
    delete $.BlobDeleteType.enum;
    $.BlobDeleteType.enum = [
        "None",
        "Permanent"
    ];
```

### Fix EncryptionAlgorithm

``` yaml
directive:
- from: swagger-document
  where: $.parameters
  transform: >
    delete $.EncryptionAlgorithm.enum;
    $.EncryptionAlgorithm.enum = [
      "None",
      "AES256"
    ];
```

### Fix XML string "ObjectReplicationMetadata" to "OrMetadata"

``` yaml
directive:
- from: swagger-document
  where: $.definitions
  transform: >
    $.BlobItemInternal.properties["OrMetadata"] = $.BlobItemInternal.properties["ObjectReplicationMetadata"];
    delete $.BlobItemInternal.properties["ObjectReplicationMetadata"];
```

# Export various createRequest/HandleResponse methods

``` yaml
directive:
- from: zz_container_client.go
  where: $
  transform: >-
    return $.
      replace(/listBlobHierarchySegmentCreateRequest/g, function(_, s) { return `ListBlobHierarchySegmentCreateRequest` }).
      replace(/listBlobHierarchySegmentHandleResponse/g, function(_, s) { return `ListBlobHierarchySegmentHandleResponse` });

- from: zz_pageblob_client.go
  where: $
  transform: >-
    return $.
      replace(/getPageRanges(Diff)?CreateRequest/g, function(_, s) { if (s === undefined) { s = '' }; return `GetPageRanges${s}CreateRequest` }).
      replace(/getPageRanges(Diff)?HandleResponse/g, function(_, s) { if (s === undefined) { s = '' }; return `GetPageRanges${s}HandleResponse` });
```

### Clean up some const type names so they don't stutter

``` yaml
directive:
- from: swagger-document
  where: $.parameters['BlobDeleteType']
  transform: >
    $["x-ms-enum"].name = "DeleteType";
    $["x-ms-client-name"] = "DeleteType";

- from: swagger-document
  where: $.parameters['BlobExpiryOptions']
  transform: >
    $["x-ms-enum"].name = "ExpiryOptions";
    $["x-ms-client-name"].name = "ExpiryOptions";

- from: swagger-document
  where: $["x-ms-paths"][*].*.responses[*].headers["x-ms-immutability-policy-mode"]
  transform: >
    $["x-ms-client-name"].name = "ImmutabilityPolicyMode";
    $.enum = [ "Mutable", "Unlocked", "Locked"];
    $["x-ms-enum"] = { "name": "ImmutabilityPolicyMode", "modelAsString": false };

- from: swagger-document
  where: $.parameters['ImmutabilityPolicyMode']
  transform: >
    $["x-ms-enum"].name = "ImmutabilityPolicySetting";
    $["x-ms-client-name"].name = "ImmutabilityPolicySetting";

- from: swagger-document
  where: $.definitions['BlobPropertiesInternal']
  transform: >
    $.properties.ImmutabilityPolicyMode["x-ms-enum"].name = "ImmutabilityPolicyMode";
```

### use azcore.ETag

``` yaml
directive:
- from: zz_models.go
  where: $
  transform: >-
    return $.
      replace(/import "time"/, `import (\n\t"time"\n\t"github.com/Azure/azure-sdk-for-go/sdk/azcore"\n)`).
      replace(/Etag\s+\*string/g, `ETag *azcore.ETag`).
      replace(/IfMatch\s+\*string/g, `IfMatch *azcore.ETag`).
      replace(/IfNoneMatch\s+\*string/g, `IfNoneMatch *azcore.ETag`).
      replace(/SourceIfMatch\s+\*string/g, `SourceIfMatch *azcore.ETag`).
      replace(/SourceIfNoneMatch\s+\*string/g, `SourceIfNoneMatch *azcore.ETag`);

- from: zz_response_types.go
  where: $
  transform: >-
    return $.
      replace(/"time"/, `"time"\n\t"github.com/Azure/azure-sdk-for-go/sdk/azcore"`).
      replace(/ETag\s+\*string/g, `ETag *azcore.ETag`);

- from:
  - zz_appendblob_client.go
  - zz_blob_client.go
  - zz_blockblob_client.go
  - zz_container_client.go
  - zz_pageblob_client.go
  where: $
  transform: >-
    return $.
      replace(/"github\.com\/Azure\/azure\-sdk\-for\-go\/sdk\/azcore\/policy"/, `"github.com/Azure/azure-sdk-for-go/sdk/azcore"\n\t"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"`).
      replace(/result\.ETag\s+=\s+&val/g, `result.ETag = (*azcore.ETag)(&val)`).
      replace(/\*modifiedAccessConditions.IfMatch/g, `string(*modifiedAccessConditions.IfMatch)`).
      replace(/\*modifiedAccessConditions.IfNoneMatch/g, `string(*modifiedAccessConditions.IfNoneMatch)`).
      replace(/\*sourceModifiedAccessConditions.SourceIfMatch/g, `string(*sourceModifiedAccessConditions.SourceIfMatch)`).
      replace(/\*sourceModifiedAccessConditions.SourceIfNoneMatch/g, `string(*sourceModifiedAccessConditions.SourceIfNoneMatch)`);
```

### Unsure why this casing changed, but fixing it

``` yaml
directive:
- from: zz_models.go
  where: $
  transform: >-
    return $.
      replace(/SignedOid\s+\*string/g, `SignedOID *string`).
      replace(/SignedTid\s+\*string/g, `SignedTID *string`);
```

### Fixing Typo with StorageErrorCodeIncrementalCopyOfEarlierVersionSnapshotNotAllowed

``` yaml
directive:
- from: zz_constants.go
  where: $
  transform: >-
    return $.
      replace(/StorageErrorCodeIncrementalCopyOfEralierVersionSnapshotNotAllowed\t+\StorageErrorCode\s+=\s+\"IncrementalCopyOfEralierVersionSnapshotNotAllowed"\n, /StorageErrorCodeIncrementalCopyOfEarlierVersionSnapshotNotAllowed\t+\StorageErrorCode\s+=\s+\"IncrementalCopyOfEarlierVersionSnapshotNotAllowed"\
      replace(/StorageErrorCodeIncrementalCopyOfEarlierVersionSnapshotNotAllowed/g, /StorageErrorCodeIncrementalCopyOfEarlierVersionSnapshotNotAllowed/g)
```
