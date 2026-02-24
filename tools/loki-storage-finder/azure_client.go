package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

// azureObjectClient implements client.ObjectClient using the modern Azure SDK
// with DefaultAzureCredential, which supports `az login`.
//
// It replicates the auth behavior of `az storage blob list`: use the management
// API to fetch storage account keys, then use SharedKeyCredential for data
// plane access. This avoids the need for `Storage Blob Data Reader` RBAC role.
type azureObjectClient struct {
	containerClient *container.Client
}

func newAzureObjectClient(accountName, containerName, endpointSuffix string) (*azureObjectClient, error) {
	if endpointSuffix == "" {
		endpointSuffix = "blob.core.windows.net"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure default credential: %w", err)
	}

	containerURL := fmt.Sprintf("https://%s.%s/%s", accountName, endpointSuffix, containerName)

	// Try to fetch the storage account key via the ARM management API,
	// which is what `az storage blob list` does under the hood.
	key, err := fetchStorageAccountKey(ctx, cred, accountName)
	if err != nil {
		// Fall back to token-based auth (requires Storage Blob Data Reader RBAC).
		fmt.Fprintf(os.Stderr, "warning: could not fetch storage account key via ARM API (%v), falling back to token auth\n", err)
		cc, err := container.NewClient(containerURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("azure container client: %w", err)
		}
		return &azureObjectClient{containerClient: cc}, nil
	}

	sharedKeyCred, err := container.NewSharedKeyCredential(accountName, key)
	if err != nil {
		return nil, fmt.Errorf("azure shared key credential: %w", err)
	}
	cc, err := container.NewClientWithSharedKeyCredential(containerURL, sharedKeyCred, nil)
	if err != nil {
		return nil, fmt.Errorf("azure container client with shared key: %w", err)
	}

	return &azureObjectClient{containerClient: cc}, nil
}

// fetchStorageAccountKey uses the Azure ARM REST API to discover the storage
// account and retrieve its access key. This replicates what `az storage blob
// list` does: use management-plane credentials to get the data-plane key.
func fetchStorageAccountKey(ctx context.Context, cred *azidentity.DefaultAzureCredential, accountName string) (string, error) {
	// Get a management-scoped token.
	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://management.azure.com/.default"},
	})
	if err != nil {
		return "", fmt.Errorf("getting management token: %w", err)
	}
	authHeader := "Bearer " + token.Token
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// 1. List subscriptions.
	type subscription struct {
		SubscriptionID string `json:"subscriptionId"`
		DisplayName    string `json:"displayName"`
	}
	type subsResponse struct {
		Value []subscription `json:"value"`
	}

	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://management.azure.com/subscriptions?api-version=2022-12-01", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", authHeader)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("listing subscriptions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("listing subscriptions: HTTP %d", resp.StatusCode)
	}

	var subs subsResponse
	if err := json.NewDecoder(resp.Body).Decode(&subs); err != nil {
		return "", fmt.Errorf("decoding subscriptions: %w", err)
	}

	// 2. For each subscription, find the storage account.
	type storageAccount struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type storageListResponse struct {
		Value []storageAccount `json:"value"`
	}

	for _, sub := range subs.Value {
		url := fmt.Sprintf(
			"https://management.azure.com/subscriptions/%s/providers/Microsoft.Storage/storageAccounts?api-version=2023-05-01",
			sub.SubscriptionID,
		)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", authHeader)
		resp, err := httpClient.Do(req)
		if err != nil {
			continue
		}

		var accounts storageListResponse
		decErr := json.NewDecoder(resp.Body).Decode(&accounts)
		resp.Body.Close()
		if decErr != nil {
			continue
		}

		for _, acct := range accounts.Value {
			if acct.Name != accountName {
				continue
			}

			// 3. Found it — get the keys.
			keysURL := fmt.Sprintf(
				"https://management.azure.com%s/listKeys?api-version=2023-05-01",
				acct.ID,
			)
			req, err := http.NewRequestWithContext(ctx, "POST", keysURL, nil)
			if err != nil {
				return "", fmt.Errorf("creating listKeys request: %w", err)
			}
			req.Header.Set("Authorization", authHeader)
			req.Header.Set("Content-Length", "0")
			resp, err := httpClient.Do(req)
			if err != nil {
				return "", fmt.Errorf("fetching storage keys: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return "", fmt.Errorf("listKeys: HTTP %d", resp.StatusCode)
			}

			type storageKey struct {
				Value string `json:"value"`
			}
			type keysResponse struct {
				Keys []storageKey `json:"keys"`
			}

			var keys keysResponse
			if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
				return "", fmt.Errorf("decoding storage keys: %w", err)
			}
			if len(keys.Keys) == 0 {
				return "", fmt.Errorf("no keys found for storage account %s", accountName)
			}

			return keys.Keys[0].Value, nil
		}
	}

	return "", fmt.Errorf("storage account %q not found in any accessible subscription", accountName)
}

func (a *azureObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	blobClient := a.containerClient.NewBlobClient(objectKey)
	_, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		if strings.Contains(err.Error(), "BlobNotFound") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (a *azureObjectClient) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	blobClient := a.containerClient.NewBlobClient(objectKey)
	props, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		return client.ObjectAttributes{}, err
	}
	var size int64
	if props.ContentLength != nil {
		size = *props.ContentLength
	}
	return client.ObjectAttributes{Size: size}, nil
}

func (a *azureObjectClient) PutObject(_ context.Context, _ string, _ io.Reader) error {
	return fmt.Errorf("PutObject not supported in read-only azure client")
}

func (a *azureObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	blobClient := a.containerClient.NewBlobClient(objectKey)
	resp, err := blobClient.DownloadStream(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}
	return resp.Body, size, nil
}

func (a *azureObjectClient) GetObjectRange(ctx context.Context, objectKey string, off, length int64) (io.ReadCloser, error) {
	// Not needed for this tool, but required by interface.
	return nil, fmt.Errorf("GetObjectRange not supported in read-only azure client")
}

func (a *azureObjectClient) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var objects []client.StorageObject
	var prefixes []client.StorageCommonPrefix

	if delimiter != "" {
		pager := a.containerClient.NewListBlobsHierarchyPager(delimiter, &container.ListBlobsHierarchyOptions{
			Prefix: &prefix,
		})
		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				return nil, nil, err
			}
			if resp.Segment != nil {
				for _, item := range resp.Segment.BlobItems {
					var modTime time.Time
					if item.Properties != nil && item.Properties.LastModified != nil {
						modTime = *item.Properties.LastModified
					}
					objects = append(objects, client.StorageObject{
						Key:        *item.Name,
						ModifiedAt: modTime,
					})
				}
				for _, p := range resp.Segment.BlobPrefixes {
					prefixes = append(prefixes, client.StorageCommonPrefix(*p.Name))
				}
			}
		}
	} else {
		pager := a.containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
			Prefix: &prefix,
		})
		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				return nil, nil, err
			}
			if resp.Segment != nil {
				for _, item := range resp.Segment.BlobItems {
					var modTime time.Time
					if item.Properties != nil && item.Properties.LastModified != nil {
						modTime = *item.Properties.LastModified
					}
					objects = append(objects, client.StorageObject{
						Key:        *item.Name,
						ModifiedAt: modTime,
					})
				}
			}
		}
	}

	return objects, prefixes, nil
}

func (a *azureObjectClient) DeleteObject(_ context.Context, _ string) error {
	return fmt.Errorf("DeleteObject not supported in read-only azure client")
}

func (a *azureObjectClient) IsObjectNotFoundErr(err error) bool {
	return strings.Contains(err.Error(), "BlobNotFound")
}

func (a *azureObjectClient) IsRetryableErr(_ error) bool {
	return false
}

func (a *azureObjectClient) Stop() {}
