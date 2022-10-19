# Code Generation - Azure Blob SDK for Golang

<!-- autorest --use=@autorest/go@4.0.0-preview.35 https://raw.githubusercontent.com/Azure/azure-rest-api-specs/main/specification/storage/data-plane/Microsoft.BlobStorage/preview/2020-10-02/blob.json --file-prefix="zz_generated_" --modelerfour.lenient-model-deduplication --license-header=MICROSOFT_MIT_NO_VERSION --output-folder=generated/ --module=azblob --openapi-type="data-plane" --credential-scope=none -->

```bash
cd swagger
autorest autorest.md
gofmt -w generated/*
```

### Settings

```yaml
go: true
clear-output-folder: false
version: "^3.0.0"
license-header: MICROSOFT_MIT_NO_VERSION
input-file: "https://raw.githubusercontent.com/Azure/azure-rest-api-specs/main/specification/storage/data-plane/Microsoft.BlobStorage/preview/2020-10-02/blob.json"
module: "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
credential-scope: "https://storage.azure.com/.default"
output-folder: internal/
file-prefix: "zz_generated_"
openapi-type: "data-plane"
verbose: true
security: AzureKey
module-version: "0.3.0"
modelerfour:
  group-parameters: false
  seal-single-value-enum-by-default: true
  lenient-model-deduplication: true
export-clients: false
use: "@autorest/go@4.0.0-preview.36"
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
