# Azure Blob Storage SDK for Go

## Introduction

The Microsoft Azure Storage SDK for Go allows you to build applications that takes advantage of Azure's scalable cloud
storage. This is the new beta client module for Azure Blob Storage, which follows
our [Azure SDK Design Guidelines for Go](https://azure.github.io/azure-sdk/golang_introduction.html) and replaces the
previous beta [azblob package](https://github.com/azure/azure-storage-blob-go).

## Getting Started

The Azure Blob SDK can access an Azure Storage account.

### Prerequisites

* Go versions 1.18 or higher
* You must have an [Azure storage account][azure_storage_account]. If you need to create one, you can use
  the [Azure Cloud Shell](https://shell.azure.com/bash) to create one with these commands (replace `my-resource-group`
  and `mystorageaccount` with your own unique names):
  (Optional) if you want a new resource group to hold the Storage Account:
  ```
  az group create --name my-resource-group --location westus2
  ```
  Create the storage account:
  ```
  az storage account create --resource-group my-resource-group --name mystorageaccount
  ```

  The storage account name can be queried with:
  ```
  az storage account show -n mystorageaccount -g my-resource-group --query "primaryEndpoints.blob"
  ```
  You can set this as an environment variable with:
  ```bash
  # PowerShell
  $ENV:AZURE_STORAGE_ACCOUNT_NAME="mystorageaccount"
  # bash
  export AZURE_STORAGE_ACCOUNT_NAME="mystorageaccount"
  ```

  Query your storage account keys:
  ```
  az storage account keys list --resource-group my-resource-group -n mystorageaccount
  ```

  Output:
  ```json
  [
      {
          "creationTime": "2022-02-07T17:18:44.088870+00:00",
          "keyName": "key1",
          "permissions": "FULL",
          "value": "..."
      },
      {
          "creationTime": "2022-02-07T17:18:44.088870+00:00",
          "keyName": "key2",
          "permissions": "FULL",
          "value": "..."
      }
  ]
  ```

  ```bash
  # PowerShell
  $ENV:AZURE_STORAGE_ACCOUNT_KEY="<mystorageaccountkey>"
  # Bash
  export AZURE_STORAGE_ACCOUNT_KEY="<mystorageaccountkey>"
  ```
  > You can obtain your account key from the Azure Portal under the "Access Keys" section on the left-hand pane of your storage account.

#### Create account

* To create a new Storage account, you can use [Azure Portal][azure_portal_create_account]
  , [Azure PowerShell][azure_powershell_create_account], or [Azure CLI][azure_cli_create_account].

### Install the package

* Install the Azure Blob Storage client module for Go with `go get`:

```bash
go get github.com/Azure/azure-sdk-for-go/sdk/storage/azblob
```

> Optional: If you are going to use AAD authentication, install the `azidentity` package:

```bash
go get github.com/Azure/azure-sdk-for-go/sdk/azidentity
```

#### Create the client

`azblob` allows you to interact with three types of resources :-

* [Azure storage accounts][azure_storage_account].
* [Containers](https://azure.microsoft.com/en-in/overview/what-is-a-container/#overview) within those storage accounts.
* [Blobs](https://azure.microsoft.com/en-in/services/storage/blobs/#overview) (block blobs/ page blobs/ append blobs)
  within those containers.

Interaction with these resources starts with an instance of a [client](#clients). To create a client object, you will
need the account's blob service endpoint URL and a credential that allows you to access the account. The `endpoint` can
be found on the page for your storage account in the [Azure Portal][azure_portal_account_url] under the "Access Keys"
section or by running the following Azure CLI command:

```bash
# Get the blob service URL for the account
az storage account show -n mystorageaccount -g my-resource-group --query "primaryEndpoints.blob"
```

Once you have the account URL, it can be used to create the service client:

```golang
cred, err := azblob.NewSharedKeyCredential("myAccountName", "myAccountKey")
handle(err)
serviceClient, err := azblob.NewServiceClientWithSharedKey("https://<myAccountName>.blob.core.windows.net/", cred, nil)
handle(err)
```

For more information about blob service URL's and how to configure custom domain names for Azure Storage check out
the [official documentation][azure_portal_account_url]

#### Types of credentials

The azblob clients support authentication via Shared Key Credential, Connection String, Shared Access Signature, or any
of the `azidentity` types that implement the `azcore.TokenCredential` interface.

##### 1. Creating the client from a shared key

To use an account [shared key][azure_shared_key] (aka account key or access key), provide the key as a string. This can
be found in your storage account in the [Azure Portal][azure_portal_account_url] under the "Access Keys" section or by
running the following Azure CLI command:

```bash
az storage account keys list -g my-resource-group -n mystorageaccount
```

Use Shared Key authentication as the credential parameter to authenticate the client:

```golang
credential, err := azblob.NewSharedKeyCredential("accountName", "accountKey")
handle(err)
serviceClient, err := azblob.NewServiceClientWithSharedKey("https://<myAccountName>.blob.core.windows.net/", credential, nil)
handle(err)
```

##### 2. Creating the client from a connection string

You can use connection string, instead of providing the account URL and credential separately, for authentication as
well. To do this, pass the connection string to the client's `NewServiceClientFromConnectionString` method. The
connection string can be found in your storage account in the [Azure Portal][azure_portal_account_url] under the "Access
Keys" section or with the following Azure CLI command:

```bash
az storage account show-connection-string -g my-resource-group -n mystorageaccount
```

```golang
connStr := "DefaultEndpointsProtocol=https;AccountName=<myAccountName>;AccountKey=<myAccountKey>;EndpointSuffix=core.windows.net"
serviceClient, err := azblob.NewServiceClientFromConnectionString(connStr, nil)
```

##### 3. Creating the client from a SAS token

To use a [shared access signature (SAS) token][azure_sas_token], provide the token as a string. You can generate a SAS
token from the Azure Portal
under [Shared access signature](https://docs.microsoft.com/rest/api/storageservices/create-service-sas) or use
the `ServiceClient.GetSASToken` or `ContainerClient.GetSASToken()` methods.

```golang
credential, err := azblob.NewSharedKeyCredential("accountName", "accountKey")
handle(err)
serviceClient, err := azblob.NewServiceClientWithSharedKey(fmt.Sprintf("https://%s.blob.core.windows.net/", accountName), credential, nil)
handle(err)
// Provide the convenience function with relevant info (services, resource types, permissions, and duration)
// The SAS token will be valid from this moment onwards.
accountSAS, err := serviceClient.GetSASToken(AccountSASResourceTypes{Object: true, Service: true, Container: true},
AccountSASPermissions{Read: true, List: true}, AccountSASServices{Blob: true}, time.Now(), time.Now().Add(48*time.Hour))
handle(err)
sasURL := fmt.Sprintf("https://%s.blob.core.windows.net/?%s", accountName, accountSAS)

// The sasURL can be used to authenticate a client without need for a credential
serviceClient, err = NewServiceClientWithNoCredential(sasURL, nil)
handle(err)
```

### Clients

Three different clients are provided to interact with the various components of the Blob Service:

1. **`ServiceClient`**
    * Get and set account settings.
    * Query, create, and delete containers within the account.

2. **`ContainerClient`**
    * Get and set container access settings, properties, and metadata.
    * Create, delete, and query blobs within the container.
    * `ContainerLeaseClient` to support container lease management.

3. **`BlobClient`**
    * `AppendBlobClient`, `BlockBlobClient`, and `PageBlobClient`
    * Get and set blob properties.
    * Perform CRUD operations on a given blob.
    * `BlobLeaseClient` to support blob lease management.

### Example

```go
// Use your storage account's name and key to create a credential object, used to access your account.
// You can obtain these details from the Azure Portal.
accountName, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_NAME")
if !ok {
    handle(errors.New("AZURE_STORAGE_ACCOUNT_NAME could not be found"))
}

accountKey, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_KEY")
if !ok {
    handle(errors.New("AZURE_STORAGE_ACCOUNT_KEY could not be found"))
}
cred, err := NewSharedKeyCredential(accountName, accountKey)
handle(err)

// Open up a service client.
// You'll need to specify a service URL, which for blob endpoints usually makes up the syntax http(s)://<account>.blob.core.windows.net/
service, err := NewServiceClientWithSharedKey(fmt.Sprintf("https://%s.blob.core.windows.net/", accountName), cred, nil)
handle(err)

// All operations in the Azure Blob Storage SDK for Go operate on a context.Context, allowing you to control cancellation/timeout.
ctx := context.Background() // This example has no expiry.

// This example showcases several common operations to help you get started, such as:

// ===== 1. Creating a container =====

// First, branch off of the service client and create a container client.
container := service.NewContainerClient("mycontainer")

// Then, fire off a create operation on the container client.
// Note that, all service-side requests have an options bag attached, allowing you to specify things like metadata, public access types, etc.
// Specifying nil omits all options.
_, err = container.Create(ctx, nil)
handle(err)

// ===== 2. Uploading/downloading a block blob =====
// We'll specify our data up-front, rather than reading a file for simplicity's sake.
data := "Hello world!"

// Branch off of the container into a block blob client
blockBlob := container.NewBlockBlobClient("HelloWorld.txt")

// Upload data to the block blob
_, err = blockBlob.Upload(ctx, NopCloser(strings.NewReader(data)), nil)
handle(err)

// Download the blob's contents and ensure that the download worked properly
get, err := blockBlob.Download(ctx, nil)
handle(err)

// Open a buffer, reader, and then download!
downloadedData := &bytes.Buffer{}
// RetryReaderOptions has a lot of in-depth tuning abilities, but for the sake of simplicity, we'll omit those here.
reader := get.Body(RetryReaderOptions{})
_, err = downloadedData.ReadFrom(reader)
handle(err)
err = reader.Close()
handle(err)
if data != downloadedData.String() {
    handle(errors.New("downloaded data doesn't match uploaded data"))
}

// ===== 3. list blobs =====
// The ListBlobs and ListContainers APIs return two channels, a values channel, and an errors channel.
// You should enumerate on a range over the values channel, and then check the errors channel, as only ONE value will ever be passed to the errors channel.
// The AutoPagerTimeout defines how long it will wait to place into the items channel before it exits & cleans itself up. A zero time will result in no timeout.
pager := container.ListBlobsFlat(nil)

for pager.NextPage(ctx) {
    resp := pager.PageResponse()

    for _, v := range resp.ContainerListBlobFlatSegmentResult.Segment.BlobItems {
        fmt.Println(*v.Name)
    }
}

if err = pager.Err(); err != nil {
    handle(err)
}

// Delete the blob we created earlier.
_, err = blockBlob.Delete(ctx, nil)
handle(err)

// Delete the container we created earlier.
_, err = container.Delete(ctx, nil)
handle(err)
```

## Troubleshooting

### Error Handling

All I/O operations will return an `error` that can be investigated to discover more information about the error. In
addition, you can investigate the raw response of any response object:

```golang
var storageErr *azblob.StorageError
resp, err := serviceClient.CreateContainer(context.Background(), "testcontainername", nil)
if err != nil && errors.As(err, &storageErr) {
    // do something with storageErr.Response()
}
```

### Logging

This module uses the classification based logging implementation in azcore. To turn on logging
set `AZURE_SDK_GO_LOGGING` to `all`.

If you only want to include logs for `azblob`, you must create your own logger and set the log classification
as `LogCredential`.

To obtain more detailed logging, including request/response bodies and header values, make sure to leave the logger as
default or enable the `LogRequest` and/or `LogResponse` classificatons. A logger that only includes credential logs can
be like the following:

```golang
import azlog "github.com/Azure/azure-sdk-for-go/sdk/azcore/log"
// Set log to output to the console
azlog.SetListener(func (cls azlog.Classification, msg string) {
    fmt.Println(msg) // printing log out to the console
})

// Includes only requests and responses in credential logs
azlog.SetClassifications(azlog.Request, azlog.Response)
```

> CAUTION: logs from credentials contain sensitive information.
> These logs must be protected to avoid compromising account security.
>

## License

This project is licensed under MIT.

## Provide Feedback

If you encounter bugs or have suggestions, please
[open an issue](https://github.com/Azure/azure-sdk-for-go/issues) and assign the `Azure.AzBlob` label.

## Contributing

This project welcomes contributions and suggestions. Most contributions require you to agree to a Contributor License
Agreement (CLA) declaring that you have the right to, and actually do, grant us the rights to use your contribution. For
details, visit https://cla.microsoft.com.

When you submit a pull request, a CLA-bot will automatically determine whether you need to provide a CLA and decorate
the PR appropriately (e.g., label, comment). Simply follow the instructions provided by the bot. You will only need to
do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.


<!-- LINKS -->

[azure_subscription]:https://azure.microsoft.com/free/

[azure_storage_account]:https://docs.microsoft.com/azure/storage/common/storage-account-create?tabs=azure-portal

[azure_portal_create_account]:https://docs.microsoft.com/azure/storage/common/storage-account-create?tabs=azure-portal

[azure_powershell_create_account]:https://docs.microsoft.com/azure/storage/common/storage-account-create?tabs=azure-powershell

[azure_cli_create_account]: https://docs.microsoft.com/azure/storage/common/storage-account-create?tabs=azure-cli

[azure_cli_account_url]:https://docs.microsoft.com/cli/azure/storage/account?view=azure-cli-latest#az-storage-account-show

[azure_powershell_account_url]:https://docs.microsoft.com/powershell/module/az.storage/get-azstorageaccount?view=azps-4.6.1

[azure_portal_account_url]:https://docs.microsoft.com/azure/storage/common/storage-account-overview#storage-account-endpoints

[azure_sas_token]:https://docs.microsoft.com/azure/storage/common/storage-sas-overview

[azure_shared_key]:https://docs.microsoft.com/rest/api/storageservices/authorize-with-shared-key

[azure_core_ref_docs]:https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azcore

[azure_core_readme]: https://github.com/Azure/azure-sdk-for-go/blob/main/sdk/azcore/README.md

[blobs_error_codes]: https://docs.microsoft.com/en-us/rest/api/storageservices/blob-service-error-codes

[msft_oss_coc]:https://opensource.microsoft.com/codeofconduct/

[msft_oss_coc_faq]:https://opensource.microsoft.com/codeofconduct/faq/

[contact_msft_oss]:mailto:opencode@microsoft.com

[blobs_rest]: https://docs.microsoft.com/en-us/rest/api/storageservices/blob-service-rest-api
