# MinIO Go Client SDK for Amazon S3 Compatible Cloud Storage [![Slack](https://slack.min.io/slack?type=svg)](https://slack.min.io) [![Sourcegraph](https://sourcegraph.com/github.com/minio/minio-go/-/badge.svg)](https://sourcegraph.com/github.com/minio/minio-go?badge) [![Apache V2 License](https://img.shields.io/badge/license-Apache%20V2-blue.svg)](https://github.com/minio/minio-go/blob/master/LICENSE)

The MinIO Go Client SDK provides simple APIs to access any Amazon S3 compatible object storage.

This quickstart guide will show you how to install the MinIO client SDK, connect to MinIO, and provide a walkthrough for a simple file uploader. For a complete list of APIs and examples, please take a look at the [Go Client API Reference](https://min.io/docs/minio/linux/developers/go/API.html).

This document assumes that you have a working [Go development environment](https://golang.org/doc/install).

## Download from Github
```sh
go get github.com/minio/minio-go/v7
```

## Initialize MinIO Client
MinIO client requires the following four parameters specified to connect to an Amazon S3 compatible object storage.

| Parameter  | Description|
| :---         |     :---     |
| endpoint   | URL to object storage service.   |
| _minio.Options_ | All the options such as credentials, custom transport etc. |

```go
package main

import (
	"log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func main() {
	endpoint := "play.min.io"
	accessKeyID := "Q3AM3UQ867SPQQA43P2F"
	secretAccessKey := "zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG"
	useSSL := true

	// Initialize minio client object.
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("%#v\n", minioClient) // minioClient is now set up
}
```

## Quick Start Example - File Uploader
This example program connects to an object storage server, creates a bucket and uploads a file to the bucket.

We will use the MinIO server running at [https://play.min.io](https://play.min.io) in this example. Feel free to use this service for testing and development. Access credentials shown in this example are open to the public.

### FileUploader.go
```go
package main

import (
	"context"
	"log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func main() {
	ctx := context.Background()
	endpoint := "play.min.io"
	accessKeyID := "Q3AM3UQ867SPQQA43P2F"
	secretAccessKey := "zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG"
	useSSL := true

	// Initialize minio client object.
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		log.Fatalln(err)
	}

	// Make a new bucket called mymusic.
	bucketName := "mymusic"
	location := "us-east-1"

	err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: location})
	if err != nil {
		// Check to see if we already own this bucket (which happens if you run this twice)
		exists, errBucketExists := minioClient.BucketExists(ctx, bucketName)
		if errBucketExists == nil && exists {
			log.Printf("We already own %s\n", bucketName)
		} else {
			log.Fatalln(err)
		}
	} else {
		log.Printf("Successfully created %s\n", bucketName)
	}

	// Upload the zip file
	objectName := "golden-oldies.zip"
	filePath := "/tmp/golden-oldies.zip"
	contentType := "application/zip"

	// Upload the zip file with FPutObject
	info, err := minioClient.FPutObject(ctx, bucketName, objectName, filePath, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("Successfully uploaded %s of size %d\n", objectName, info.Size)
}
```

### Run FileUploader
```sh
go run file-uploader.go
2016/08/13 17:03:28 Successfully created mymusic
2016/08/13 17:03:40 Successfully uploaded golden-oldies.zip of size 16253413

mc ls play/mymusic/
[2016-05-27 16:02:16 PDT]  17MiB golden-oldies.zip
```

## API Reference
The full API Reference is available here.

* [Complete API Reference](https://min.io/docs/minio/linux/developers/go/API.html)

### API Reference : Bucket Operations
* [`MakeBucket`](https://min.io/docs/minio/linux/developers/go/API.html#MakeBucket)
* [`ListBuckets`](https://min.io/docs/minio/linux/developers/go/API.html#ListBuckets)
* [`BucketExists`](https://min.io/docs/minio/linux/developers/go/API.html#BucketExists)
* [`RemoveBucket`](https://min.io/docs/minio/linux/developers/go/API.html#RemoveBucket)
* [`ListObjects`](https://min.io/docs/minio/linux/developers/go/API.html#ListObjects)
* [`ListIncompleteUploads`](https://min.io/docs/minio/linux/developers/go/API.html#ListIncompleteUploads)

### API Reference : Bucket policy Operations
* [`SetBucketPolicy`](https://min.io/docs/minio/linux/developers/go/API.html#SetBucketPolicy)
* [`GetBucketPolicy`](https://min.io/docs/minio/linux/developers/go/API.html#GetBucketPolicy)

### API Reference : Bucket notification Operations
* [`SetBucketNotification`](https://min.io/docs/minio/linux/developers/go/API.html#SetBucketNotification)
* [`GetBucketNotification`](https://min.io/docs/minio/linux/developers/go/API.html#GetBucketNotification)
* [`RemoveAllBucketNotification`](https://min.io/docs/minio/linux/developers/go/API.html#RemoveAllBucketNotification)
* [`ListenBucketNotification`](https://min.io/docs/minio/linux/developers/go/API.html#ListenBucketNotification) (MinIO Extension)
* [`ListenNotification`](https://min.io/docs/minio/linux/developers/go/API.html#ListenNotification) (MinIO Extension)

### API Reference : File Object Operations
* [`FPutObject`](https://min.io/docs/minio/linux/developers/go/API.html#FPutObject)
* [`FGetObject`](https://min.io/docs/minio/linux/developers/go/API.html#FGetObject)

### API Reference : Object Operations
* [`GetObject`](https://min.io/docs/minio/linux/developers/go/API.html#GetObject)
* [`PutObject`](https://min.io/docs/minio/linux/developers/go/API.html#PutObject)
* [`PutObjectStreaming`](https://min.io/docs/minio/linux/developers/go/API.html#PutObjectStreaming)
* [`StatObject`](https://min.io/docs/minio/linux/developers/go/API.html#StatObject)
* [`CopyObject`](https://min.io/docs/minio/linux/developers/go/API.html#CopyObject)
* [`RemoveObject`](https://min.io/docs/minio/linux/developers/go/API.html#RemoveObject)
* [`RemoveObjects`](https://min.io/docs/minio/linux/developers/go/API.html#RemoveObjects)
* [`RemoveIncompleteUpload`](https://min.io/docs/minio/linux/developers/go/API.html#RemoveIncompleteUpload)
* [`SelectObjectContent`](https://min.io/docs/minio/linux/developers/go/API.html#SelectObjectContent)


### API Reference : Presigned Operations
* [`PresignedGetObject`](https://min.io/docs/minio/linux/developers/go/API.html#PresignedGetObject)
* [`PresignedPutObject`](https://min.io/docs/minio/linux/developers/go/API.html#PresignedPutObject)
* [`PresignedHeadObject`](https://min.io/docs/minio/linux/developers/go/API.html#PresignedHeadObject)
* [`PresignedPostPolicy`](https://min.io/docs/minio/linux/developers/go/API.html#PresignedPostPolicy)

### API Reference : Client custom settings
* [`SetAppInfo`](https://min.io/docs/minio/linux/developers/go/API.html#SetAppInfo)
* [`TraceOn`](https://min.io/docs/minio/linux/developers/go/API.html#TraceOn)
* [`TraceOff`](https://min.io/docs/minio/linux/developers/go/API.html#TraceOff)

## Full Examples

### Full Examples : Bucket Operations
* [makebucket.go](https://github.com/minio/minio-go/blob/master/examples/s3/makebucket.go)
* [listbuckets.go](https://github.com/minio/minio-go/blob/master/examples/s3/listbuckets.go)
* [bucketexists.go](https://github.com/minio/minio-go/blob/master/examples/s3/bucketexists.go)
* [removebucket.go](https://github.com/minio/minio-go/blob/master/examples/s3/removebucket.go)
* [listobjects.go](https://github.com/minio/minio-go/blob/master/examples/s3/listobjects.go)
* [listobjectsV2.go](https://github.com/minio/minio-go/blob/master/examples/s3/listobjectsV2.go)
* [listincompleteuploads.go](https://github.com/minio/minio-go/blob/master/examples/s3/listincompleteuploads.go)

### Full Examples : Bucket policy Operations
* [setbucketpolicy.go](https://github.com/minio/minio-go/blob/master/examples/s3/setbucketpolicy.go)
* [getbucketpolicy.go](https://github.com/minio/minio-go/blob/master/examples/s3/getbucketpolicy.go)
* [listbucketpolicies.go](https://github.com/minio/minio-go/blob/master/examples/s3/listbucketpolicies.go)

### Full Examples : Bucket lifecycle Operations
* [setbucketlifecycle.go](https://github.com/minio/minio-go/blob/master/examples/s3/setbucketlifecycle.go)
* [getbucketlifecycle.go](https://github.com/minio/minio-go/blob/master/examples/s3/getbucketlifecycle.go)

### Full Examples : Bucket encryption Operations
* [setbucketencryption.go](https://github.com/minio/minio-go/blob/master/examples/s3/setbucketencryption.go)
* [getbucketencryption.go](https://github.com/minio/minio-go/blob/master/examples/s3/getbucketencryption.go)
* [deletebucketencryption.go](https://github.com/minio/minio-go/blob/master/examples/s3/deletebucketencryption.go)

### Full Examples : Bucket replication Operations
* [setbucketreplication.go](https://github.com/minio/minio-go/blob/master/examples/s3/setbucketreplication.go)
* [getbucketreplication.go](https://github.com/minio/minio-go/blob/master/examples/s3/getbucketreplication.go)
* [removebucketreplication.go](https://github.com/minio/minio-go/blob/master/examples/s3/removebucketreplication.go)

### Full Examples : Bucket notification Operations
* [setbucketnotification.go](https://github.com/minio/minio-go/blob/master/examples/s3/setbucketnotification.go)
* [getbucketnotification.go](https://github.com/minio/minio-go/blob/master/examples/s3/getbucketnotification.go)
* [removeallbucketnotification.go](https://github.com/minio/minio-go/blob/master/examples/s3/removeallbucketnotification.go)
* [listenbucketnotification.go](https://github.com/minio/minio-go/blob/master/examples/minio/listenbucketnotification.go) (MinIO Extension)
* [listennotification.go](https://github.com/minio/minio-go/blob/master/examples/minio/listen-notification.go) (MinIO Extension)

### Full Examples : File Object Operations
* [fputobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/fputobject.go)
* [fgetobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/fgetobject.go)

### Full Examples : Object Operations
* [putobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/putobject.go)
* [getobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/getobject.go)
* [statobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/statobject.go)
* [copyobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/copyobject.go)
* [removeobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/removeobject.go)
* [removeincompleteupload.go](https://github.com/minio/minio-go/blob/master/examples/s3/removeincompleteupload.go)
* [removeobjects.go](https://github.com/minio/minio-go/blob/master/examples/s3/removeobjects.go)

### Full Examples : Encrypted Object Operations
* [put-encrypted-object.go](https://github.com/minio/minio-go/blob/master/examples/s3/put-encrypted-object.go)
* [get-encrypted-object.go](https://github.com/minio/minio-go/blob/master/examples/s3/get-encrypted-object.go)
* [fput-encrypted-object.go](https://github.com/minio/minio-go/blob/master/examples/s3/fputencrypted-object.go)

### Full Examples : Presigned Operations
* [presignedgetobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/presignedgetobject.go)
* [presignedputobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/presignedputobject.go)
* [presignedheadobject.go](https://github.com/minio/minio-go/blob/master/examples/s3/presignedheadobject.go)
* [presignedpostpolicy.go](https://github.com/minio/minio-go/blob/master/examples/s3/presignedpostpolicy.go)

## Explore Further
* [Complete Documentation](https://min.io/docs/minio/kubernetes/upstream/index.html)
* [MinIO Go Client SDK API Reference](https://min.io/docs/minio/linux/developers/go/API.html)

## Contribute
[Contributors Guide](https://github.com/minio/minio-go/blob/master/CONTRIBUTING.md)

## License
This SDK is distributed under the [Apache License, Version 2.0](https://www.apache.org/licenses/LICENSE-2.0), see [LICENSE](https://github.com/minio/minio-go/blob/master/LICENSE) and [NOTICE](https://github.com/minio/minio-go/blob/master/NOTICE) for more information.
