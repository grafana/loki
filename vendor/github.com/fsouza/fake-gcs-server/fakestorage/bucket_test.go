// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"context"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"google.golang.org/api/iterator"
)

func TestServerClientBucketAttrs(t *testing.T) {
	server := NewServer([]Object{
		{BucketName: "some-bucket", Name: "img/hi-res/party-01.jpg"},
		{BucketName: "some-bucket", Name: "img/hi-res/party-02.jpg"},
		{BucketName: "some-bucket", Name: "img/hi-res/party-03.jpg"},
		{BucketName: "other-bucket", Name: "static/css/website.css"},
	})
	defer server.Stop()
	client := server.Client()
	attrs, err := client.Bucket("some-bucket").Attrs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expectedName := "some-bucket"
	if attrs.Name != expectedName {
		t.Errorf("wrong bucket name returned\nwant %q\ngot  %q", expectedName, attrs.Name)
	}
}

func TestServerClientBucketAttrsAfterCreateBucket(t *testing.T) {
	const bucketName = "best-bucket-ever"
	server := NewServer(nil)
	defer server.Stop()
	server.CreateBucket(bucketName)
	client := server.Client()
	attrs, err := client.Bucket(bucketName).Attrs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Name != bucketName {
		t.Errorf("wrong bucket name returned\nwant %q\ngot  %q", bucketName, attrs.Name)
	}
}

func TestServerClientBucketAttrsNotFound(t *testing.T) {
	server := NewServer(nil)
	defer server.Stop()
	client := server.Client()
	attrs, err := client.Bucket("some-bucket").Attrs(context.Background())
	if err == nil {
		t.Error("unexpected <nil> error")
	}
	if attrs != nil {
		t.Errorf("unexpected non-nil attrs: %#v", attrs)
	}
}

func TestServerClientListBuckets(t *testing.T) {
	server := NewServer([]Object{
		{BucketName: "some-bucket", Name: "img/hi-res/party-01.jpg"},
		{BucketName: "some-bucket", Name: "img/hi-res/party-02.jpg"},
		{BucketName: "some-bucket", Name: "img/hi-res/party-03.jpg"},
		{BucketName: "other-bucket", Name: "static/css/website.css"},
	})
	defer server.Stop()
	client := server.Client()
	it := client.Buckets(context.Background(), "whatever")
	var returnedNames []string
	b, err := it.Next()
	for ; err == nil; b, err = it.Next() {
		returnedNames = append(returnedNames, b.Name)
	}
	if err != iterator.Done {
		t.Fatal(err)
	}
	expectedNames := []string{"other-bucket", "some-bucket"}
	if !reflect.DeepEqual(returnedNames, expectedNames) {
		t.Errorf("wrong names returned\nwant %#v\ngot  %#v", expectedNames, returnedNames)
	}
}

func TestServerClientListObjects(t *testing.T) {
	objects := []Object{
		{BucketName: "some-bucket", Name: "img/hi-res/party-01.jpg"},
		{BucketName: "some-bucket", Name: "img/hi-res/party-02.jpg"},
		{BucketName: "some-bucket", Name: "img/hi-res/party-03.jpg"},
	}

	dir, err := ioutil.TempDir("", "fakestorage-test-root-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	serverOptions := []Options{
		{InitialObjects: objects},
		{InitialObjects: objects, StorageRoot: dir},
	}

	for _, options := range serverOptions {

		server, err := NewServerWithOptions(options)
		if err != nil {
			t.Error(err)
		}
		defer server.Stop()
		client := server.Client()

		seenFiles := map[string]struct{}{}

		it := client.Bucket("some-bucket").Objects(context.Background(), nil)
		objAttrs, err := it.Next()
		for ; err == nil; objAttrs, err = it.Next() {
			seenFiles[objAttrs.Name] = struct{}{}
			t.Logf("Seen file %s", objAttrs.Name)
		}

		if len(objects) != len(seenFiles) {
			t.Errorf("wrong number of files\nwant %d\ngot %d", len(objects), len(seenFiles))
		}
	}
}
