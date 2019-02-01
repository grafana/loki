// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage_test

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/fsouza/fake-gcs-server/fakestorage"
)

func ExampleServer_Client() {
	server := fakestorage.NewServer([]fakestorage.Object{
		{
			BucketName: "some-bucket",
			Name:       "some/object/file.txt",
			Content:    []byte("inside the file"),
		},
	})
	defer server.Stop()
	client := server.Client()
	object := client.Bucket("some-bucket").Object("some/object/file.txt")
	reader, err := object.NewReader(context.Background())
	if err != nil {
		panic(err)
	}
	defer reader.Close()
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s", data)
	// Output: inside the file
}

func ExampleServer_with_host_port() {
	server, err := fakestorage.NewServerWithHostPort([]fakestorage.Object{
		{
			BucketName: "some-bucket",
			Name:       "some/object/file.txt",
			Content:    []byte("inside the file"),
		},
	}, "127.0.0.1", 8081)
	if err != nil {
		panic(err)
	}
	defer server.Stop()
	client := server.Client()
	object := client.Bucket("some-bucket").Object("some/object/file.txt")
	reader, err := object.NewReader(context.Background())
	if err != nil {
		panic(err)
	}
	defer reader.Close()
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s", data)
	// Output: inside the file
}
