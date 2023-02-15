//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package base

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
)

type Client[T any] struct {
	inner     *T
	sharedKey *exported.SharedKeyCredential
}

func InnerClient[T any](client *Client[T]) *T {
	return client.inner
}

func SharedKey[T any](client *Client[T]) *exported.SharedKeyCredential {
	return client.sharedKey
}

func NewClient[T any](inner *T) *Client[T] {
	return &Client[T]{inner: inner}
}

func NewServiceClient(containerURL string, pipeline runtime.Pipeline, sharedKey *exported.SharedKeyCredential) *Client[generated.ServiceClient] {
	return &Client[generated.ServiceClient]{
		inner:     generated.NewServiceClient(containerURL, pipeline),
		sharedKey: sharedKey,
	}
}

func NewContainerClient(containerURL string, pipeline runtime.Pipeline, sharedKey *exported.SharedKeyCredential) *Client[generated.ContainerClient] {
	return &Client[generated.ContainerClient]{
		inner:     generated.NewContainerClient(containerURL, pipeline),
		sharedKey: sharedKey,
	}
}

func NewBlobClient(blobURL string, pipeline runtime.Pipeline, sharedKey *exported.SharedKeyCredential) *Client[generated.BlobClient] {
	return &Client[generated.BlobClient]{
		inner:     generated.NewBlobClient(blobURL, pipeline),
		sharedKey: sharedKey,
	}
}

type CompositeClient[T, U any] struct {
	innerT    *T
	innerU    *U
	sharedKey *exported.SharedKeyCredential
}

func InnerClients[T, U any](client *CompositeClient[T, U]) (*Client[T], *U) {
	return &Client[T]{inner: client.innerT}, client.innerU
}

func NewAppendBlobClient(blobURL string, pipeline runtime.Pipeline, sharedKey *exported.SharedKeyCredential) *CompositeClient[generated.BlobClient, generated.AppendBlobClient] {
	return &CompositeClient[generated.BlobClient, generated.AppendBlobClient]{
		innerT:    generated.NewBlobClient(blobURL, pipeline),
		innerU:    generated.NewAppendBlobClient(blobURL, pipeline),
		sharedKey: sharedKey,
	}
}

func NewBlockBlobClient(blobURL string, pipeline runtime.Pipeline, sharedKey *exported.SharedKeyCredential) *CompositeClient[generated.BlobClient, generated.BlockBlobClient] {
	return &CompositeClient[generated.BlobClient, generated.BlockBlobClient]{
		innerT:    generated.NewBlobClient(blobURL, pipeline),
		innerU:    generated.NewBlockBlobClient(blobURL, pipeline),
		sharedKey: sharedKey,
	}
}

func NewPageBlobClient(blobURL string, pipeline runtime.Pipeline, sharedKey *exported.SharedKeyCredential) *CompositeClient[generated.BlobClient, generated.PageBlobClient] {
	return &CompositeClient[generated.BlobClient, generated.PageBlobClient]{
		innerT:    generated.NewBlobClient(blobURL, pipeline),
		innerU:    generated.NewPageBlobClient(blobURL, pipeline),
		sharedKey: sharedKey,
	}
}

func SharedKeyComposite[T, U any](client *CompositeClient[T, U]) *exported.SharedKeyCredential {
	return client.sharedKey
}
