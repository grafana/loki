//go:build go1.16
// +build go1.16

// Copyright 2017 Microsoft Corporation. All rights reserved.
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

/*

Package azblob can access an Azure Blob Storage.

The azblob package is capable of :-
    - Creating, deleting, and querying containers in an account
    - Creating, deleting, and querying blobs in a container
    - Creating Shared Access Signature for authentication

Types of Resources

The azblob package allows you to interact with three types of resources :-

* Azure storage accounts.
* Containers within those storage accounts.
* Blobs (block blobs/ page blobs/ append blobs) within those containers.

The Azure Storage Blob (azblob) client library for Go allows you to interact with each of these components through the use of a dedicated client object.
To create a client object, you will need the account's blob service endpoint URL and a credential that allows you to access the account.

Types of Credentials

The clients support different forms of authentication.
The azblob library supports any of the `azcore.TokenCredential` interfaces, authorization via a Connection String,
or authorization with a Shared Access Signature token.

Using a Shared Key

To use an account shared key (aka account key or access key), provide the key as a string.
This can be found in your storage account in the Azure Portal under the "Access Keys" section.

Use the key as the credential parameter to authenticate the client:

	accountName, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_NAME")
	if !ok {
		panic("AZURE_STORAGE_ACCOUNT_NAME could not be found")
	}
	accountKey, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_KEY")
	if !ok {
		panic("AZURE_STORAGE_ACCOUNT_KEY could not be found")
	}
	credential, err := NewSharedKeyCredential(accountName, accountKey)
	handle(err)

	serviceClient, err := azblob.NewServiceClient("https://<my_account_name>.blob.core.windows.net/", cred, nil)
	handle(err)

Using a Connection String

Depending on your use case and authorization method, you may prefer to initialize a client instance with a connection string instead of providing the account URL and credential separately.
To do this, pass the connection string to the service client's `NewServiceClientFromConnectionString` method.
The connection string can be found in your storage account in the Azure Portal under the "Access Keys" section.

	connStr := "DefaultEndpointsProtocol=https;AccountName=<my_account_name>;AccountKey=<my_account_key>;EndpointSuffix=core.windows.net"
	serviceClient, err := azblob.NewServiceClientFromConnectionString(connStr, nil)

Using a Shared Access Signature (SAS) Token

To use a shared access signature (SAS) token, provide the token at the end of your service URL.
You can generate a SAS token from the Azure Portal under Shared Access Signature or use the ServiceClient.GetSASToken() functions.

	accountName, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_NAME")
	if !ok {
		panic("AZURE_STORAGE_ACCOUNT_NAME could not be found")
	}
	accountKey, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_KEY")
	if !ok {
		panic("AZURE_STORAGE_ACCOUNT_KEY could not be found")
	}
	credential, err := azblob.NewSharedKeyCredential(accountName, accountKey)
	handle(err)

	serviceClient, err := azblob.NewServiceClient(fmt.Sprintf("https://%s.blob.core.windows.net/", accountName), credential, nil)
	handle(err)

    // Provide the convenience function with relevant info
	accountSAS, err := serviceClient.GetSASToken(AccountSASResourceTypes{Object: true, Service: true, Container: true}, AccountSASPermissions{Read: true, List: true}, AccountSASServices{Blob: true}, time.Now(), time.Now().Add(48*time.Hour))
	handle(err)

	urlToSend := fmt.Sprintf("https://%s.blob.core.windows.net/?%s", accountName, accountSAS)
	// You can hand off this URL to someone else via any mechanism you choose.

	// ******************************************

	// When someone receives the URL, they can access the resource using it in code like this, or a tool of some variety.
	serviceClient, err = azblob.NewServiceClient(urlToSend, azcore.NewAnonymousCredential(), nil)
	handle(err)

	// You can also break a blob URL up into its constituent parts
	blobURLParts := azblob.NewBlobURLParts(serviceClient.URL())
	fmt.Printf("SAS expiry time = %s\n", blobURLParts.SAS.ExpiryTime())

Types of Clients

There are three different clients provided to interact with the various components of the Blob Service:

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

Examples

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
	service, err := NewServiceClient(fmt.Sprintf("https://%s.blob.core.windows.net/", accountName), cred, nil)
	handle(err)

	// All operations in the Azure Storage Blob SDK for Go operate on a context.Context, allowing you to control cancellation/timeout.
	ctx := context.Background() // This example has no expiry.

	// This example showcases several common operations to help you get started, such as:

	// ===== 1. Creating a container =====

	// First, branch off of the service client and create a container client.
	container := service.NewContainerClient("myContainer")
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
	reader := get.Body(RetryReaderOptions{}) // RetryReaderOptions has a lot of in-depth tuning abilities, but for the sake of simplicity, we'll omit those here.
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
*/

package azblob
