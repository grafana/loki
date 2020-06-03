# Promtail-Objectstore-scraping

Promtail currently supports scraping logs from local filesystem logs, syslog and systemd/journal. This covers most of the usecases where an user would potentially store logs. In addition to this there are few other usecases where the logs can be stored in an Object store and user would like to collect and aggregate those logs in Loki. AWS loadbalancer is an one such example, Loadbalancer access logs are stored in S3. It would be good addition to Promtail to support scraping logs from Object store to cover similar user cases mentioned above. 

Cortex already has the [implementation](https://github.com/cortexproject/cortex/blob/master/pkg/chunk/aws/s3_storage_client.go) for Objecstore including S3 and GCS. We can try to re-use most of it. Still, we have to develop code for `file watching` and updating the `positions.yaml` file.  Following are the few challenges which we should try to address.

### There is no appending of lines to file in object store

In an object store there is no appending of lines to a file in bucket. Any file modified will be newly 
uploaded to the bucket with the same name. So, we would have to read the entire file again from the begining since we will not be aware if only new lines were appended to the modified file or the file is completely having new content

### Where should we keep positions.yaml file ?

Since, the files we scrape is remotely placed in a objecstore, should we keep the `positions.yaml` also in the same place? Advantage of keeping the `positions.yaml` within the objecstore will help us to recover from the state where the scraping was left when promtail is running in docker container and the container crashes. However, `user` reading the files from the Object store would also need write access to the Objecstore. Getting write access may not be possible in some cases where people doesn't want to risk modifying the files by giving write access.

### How should we support reading of compressed files ?

If the files in the Objectstore is stored in a compressed format, promtail should have support for reading the compressed data and we should make sure we support most of the compression algorithms like we already do for chunks(Snappy, LZ4, Gzip) 

### How we should identify and watch newly added files ?

To identify and to start tailing the newly added files, we should list the files in the Object store at some desired intervals. How frequently we should this? Once we add the file to `watch`, to identify any changes to the file we should frequently check the modified `datetime` of the file. The list of files might be large and each watch will be running in a `go routine` and each `go routine` making frequent `http` requests to the Obejctstore. We should make sure this doesn't affect promtail's performance.

Instead of frequently doing `ListObjects`, we can consider to use event notification in S3 with `SQS`, through which we can get to know the newly added files and then we can fetch only them.

### How to fetch the file and read the contents?

Here, we have two options. One, just get the file with a `reader` and iterate the lines of the file but if the files we are fetching are very large, just fetching a file using `GetObject` will take time and we would be keeping large amount of data in memory. This might be a serious performance issue. Two, we can download the file to local and then read the file the same way we do for other log files. We should implement multipart download to make the downloading of large files faster and also we should read the downloaded file in smaller chunks to reduce memory consumption. 

## Proposed Solution for the above mentioned points

> There is no appending of lines to file in object store

In the positions.yaml along with the `offset` we should also store file modified `datetime`. If we see that date is modified we should reset the offset and tail the contents from the begining. This is the same behaviour in local filesystem as well. If a file is `opened`, `hpcloud tail` will tail the file from the begining.

#### Alternative 

Instead of tailing the file from the beging upon modification, we can calculate the `checksum` of the contents till the `offset`, and check if the checksum was changed or not after the file was modified. If it is not changed then we can just proceed further, else we can tail from the begining.

Any other better way to handle this?


> Where should we keep `positions.yaml` file ?

I think this should be an user choice. One thing we need to consider here is that there will be API limit per account per region and if we store the `positions.yaml` in the object store itself, we may end up doing lot of APIs calls and we would get `RequestLimitExceeded` very frequently. Still, backoff and retry should be implemented in case we get this error even when doing API calls less frequently. So, placing and updating the `positions.yaml` should be left to the user so he can decide based on the API usage of his setup wether to keep it locally or remotely. 

> How we should identify and watch newly added files ?

Limit on API calls should also be considered for syncing the newly added files. To start tailing the newly added files, we should do `ListObjects` at a desired interval. We should make sure we don't keep this interval too small.
Also, once the entire contents of the file is read we should stop watching the file so that we reduce the number of active `go routines`. 

> How should we support reading of compressed files ?

It would be good to support most of the compression algorithms like we already do for chunks(Snappy, LZ4, Gzip). 
**Note:** We can only support decompressing of data only if the whole file is compressed using any of the above algorithms, we cannot build support for decompressing data for custom data format stored in files.

## Example S3 config for `positions.yaml`

```yaml
positions:
  objectstore:
    aws:
      ## bucket name
      bucket: BUCKET_NAME
      
      ## endpoint for the store
      endpoint: 

      ## secret key
      secret_access_key: 

      ## access key
      access_key: 

      ## path to store positions.yaml
      filename: /path/to/positions.yaml   
      
      sync_period: 1m
```

## Sample `positions.yaml` file

```yaml
positions:
  /path/to/test.log: "66","2020-05-21T23:59:00"
```

## Other version of `positions.yaml` file

```yaml
positions:
  /path/to/test.log: 
      osffset: "66",
      latest_modified_datetime: "2020-05-21T23:59:00Z"
```

## Iterations

We will be implementing this feature in iterations. 

### A brief implementation details for the first iteration

1. We will use Cortex's object client code to list and read objects
2. `positions.yaml` will be kept locally
3. If the object is modifed, we will resume to read from the previous known position. Suppose, if the modified object's size is less than the previous know position then we read from the begining.
4. We will support scraping logs only from S3 in first iteration.
5. Only normal files and `gzip` files will be supported. 