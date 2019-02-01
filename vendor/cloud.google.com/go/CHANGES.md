# Changes

## v0.34.0

- functions/metadata:
  - Switch to using JSON in context.
  - Make Resource a value.
- vision: Fix ProductSearch return type.
- datastore: Add an example for how to handle MultiError.

## v0.33.1

- compute: Removes an erroneously added go.mod.
- logging: Populate source location in fromLogEntry.

## v0.33.0

- bttest:
  - Add support for apply_label_transformer.
- expr:
  - Add expr library.
- firestore:
  - Support retrieval of missing documents.
- kms:
  - Add IAM methods.
- pubsub:
  - Clarify extension documentation.
- scheduler:
  - Add v1beta1 client.
- vision:
  - Add product search helper.
  - Add new product search client.

## v0.32.0

Note: This release is the last to support Go 1.6 and 1.8.

- bigquery:
    - Add support for removing an expiration.
    - Ignore NeverExpire in Table.Create.
    - Validate table expiration time.
- cbt:
    - Add note about not supporting arbitrary bytes.
- datastore:
    - Align key checks.
- firestore:
    - Return an error when using Start/End without providing values.
- pubsub:
    - Add pstest Close method.
    - Clarify MaxExtension documentation.
- securitycenter:
    - Add v1beta1 client.
- spanner:
    - Allow nil in mutations.
    - Improve doc of SessionPoolConfig.MaxOpened.
    - Increase session deletion timeout from 5s to 15s.

## v0.31.0

- bigtable:
    - Group mutations across multiple requests.
- bigquery:
    - Link to bigquery troubleshooting errors page in bigquery.Error comment.
- cbt:
    - Fix go generate command.
    - Document usage of both maxage + maxversions.
- datastore:
    - Passing nil keys results in ErrInvalidKey.
- firestore:
    - Clarify what Document.DataTo does with untouched struct fields.
- profile:
    - Validate service name in agent.
- pubsub:
    - Fix deadlock with pstest and ctx.Cancel.
    - Fix a possible deadlock in pstest.
- trace:
    - Update doc URL with new fragment.

Special thanks to @fastest963 for going above and beyond helping us to debug
hard-to-reproduce Pub/Sub issues.

## v0.30.0

- spanner: DML support added. See https://godoc.org/cloud.google.com/go/spanner#hdr-DML_and_Partitioned_DML for more information.
- bigtable: bttest supports row sample filter.
- functions: metadata package added for accessing Cloud Functions resource metadata.

## v0.29.0

- bigtable:
  - Add retry to all idempotent RPCs.
  - cbt supports complex GC policies.
  - Emulator supports arbitrary bytes in regex filters.
- firestore: Add ArrayUnion and ArrayRemove.
- logging: Add the ContextFunc option to supply the context used for
  asynchronous RPCs.
- profiler: Ignore NotDefinedError when fetching the instance name
- pubsub:
  - BEHAVIOR CHANGE: Receive doesn't retry if an RPC returns codes.Cancelled.
  - BEHAVIOR CHANGE: Receive retries on Unavailable intead of returning.
  - Fix deadlock.
  - Restore Ack/Nack/Modacks metrics.
  - Improve context handling in iterator.
  - Implement synchronous mode for Receive.
  - pstest: add Pull.
- spanner: Add a metric for the number of sessions currently opened.
- storage:
  - Canceling the context releases all resources.
  - Add additional RetentionPolicy attributes.
- vision/apiv1: Add LocalizeObjects method.

## v0.28.0

- bigtable:
  - Emulator returns Unimplemented for snapshot RPCs.
- bigquery:
  - Support zero-length repeated, nested fields.
- cloud assets:
  - Add v1beta client.
- datastore:
  - Don't nil out transaction ID on retry.
- firestore:
  - BREAKING CHANGE: When watching a query with Query.Snapshots, QuerySnapshotIterator.Next
  returns a QuerySnapshot which contains read time, result size, change list and the DocumentIterator
  (previously, QuerySnapshotIterator.Next returned just the DocumentIterator). See: https://godoc.org/cloud.google.com/go/firestore#Query.Snapshots.
  - Add array-contains operator.
- IAM:
  - Add iam/credentials/apiv1 client.
- pubsub:
  - Canceling the context passed to Subscription.Receive causes Receive to return when
  processing finishes on all messages currently in progress, even if new messages are arriving.
- redis:
  - Add redis/apiv1 client.
- storage:
  - Add Reader.Attrs.
  - Deprecate several Reader getter methods: please use Reader.Attrs for these instead.
  - Add ObjectHandle.Bucket and ObjectHandle.Object methods.

## v0.27.0

- bigquery:
  - Allow modification of encryption configuration and partitioning options to a table via the Update call.
  - Add a SchemaFromJSON function that converts a JSON table schema.
- bigtable:
  - Restore cbt count functionality.
- containeranalysis:
  - Add v1beta client.
- spanner:
  - Fix a case where an iterator might not be closed correctly.
- storage:
  - Add ServiceAccount method https://godoc.org/cloud.google.com/go/storage#Client.ServiceAccount.
  - Add a method to Reader that returns the parsed value of the Last-Modified header.

## v0.26.0

- bigquery:
  - Support filtering listed jobs  by min/max creation time.
  - Support data clustering (https://godoc.org/cloud.google.com/go/bigquery#Clustering).
  - Include job creator email in Job struct.
- bigtable:
  - Add `RowSampleFilter`.
  - emulator: BREAKING BEHAVIOR CHANGE: Regexps in row, family, column and value filters
    must match the entire target string to succeed. Previously, the emulator was
    succeeding on  partial matches.
    NOTE: As of this release, this change only affects the emulator when run
    from this repo (bigtable/cmd/emulator/cbtemulator.go). The version launched
    from `gcloud` will be updated in a subsequent `gcloud` release.
- dataproc: Add apiv1beta2 client.
- datastore: Save non-nil pointer fields on omitempty.
- logging: populate Entry.Trace from the HTTP X-Cloud-Trace-Context header.
- logging/logadmin:  Support writer_identity and include_children.
- pubsub:
  - Support labels on topics and subscriptions.
  - Support message storage policy for topics.
  - Use the distribution of ack times to determine when to extend ack deadlines.
    The only user-visible effect of this change should be that programs that
    call only `Subscription.Receive` need no IAM permissions other than `Pub/Sub
    Subscriber`.
- storage:
  - Support predefined ACLs.
  - Support additional ACL fields other than Entity and Role.
  - Support bucket websites.
  - Support bucket logging.


## v0.25.0

- Added [Code of Conduct](https://github.com/GoogleCloudPlatform/google-cloud-go/blob/master/CODE_OF_CONDUCT.md)
- bigtable:
  - cbt: Support a GC policy of "never".
- errorreporting:
  - Support User.
  - Close now calls Flush.
  - Use OnError (previously ignored).
  - Pass through the RPC error as-is to OnError.
- httpreplay: A tool for recording and replaying HTTP requests
  (for the bigquery and storage clients in this repo).
- kms: v1 client added
- logging: add SourceLocation to Entry.
- storage: improve CRC checking on read.

## v0.24.0

- bigquery: Support for the NUMERIC type.
- bigtable:
  - cbt: Optionally specify columns for read/lookup
  - Support instance-level administration.
- oslogin: New client for the OS Login API.
- pubsub:
  - The package is now stable. There will be no further breaking changes.
  - Internal changes to improve Subscription.Receive behavior.
- storage: Support updating bucket lifecycle config.
- spanner: Support struct-typed parameter bindings.
- texttospeech: New client for the Text-to-Speech API.

## v0.23.0

- bigquery: Add DDL stats to query statistics.
- bigtable:
  - cbt: Add cells-per-column limit for row lookup.
  - cbt: Make it possible to combine read filters.
- dlp: v2beta2 client removed. Use the v2 client instead.
- firestore, spanner: Fix compilation errors due to protobuf changes.

## v0.22.0

- bigtable:
  - cbt: Support cells per column limit for row read.
  - bttest: Correctly handle empty RowSet.
  - Fix ReadModifyWrite operation in emulator.
  - Fix API path in GetCluster.

- bigquery:
  - BEHAVIOR CHANGE: Retry on 503 status code.
  - Add dataset.DeleteWithContents.
  - Add SchemaUpdateOptions for query jobs.
  - Add Timeline to QueryStatistics.
  - Add more stats to ExplainQueryStage.
  - Support Parquet data format.

- datastore:
  - Support omitempty for times.

- dlp:
  - **BREAKING CHANGE:** Remove v1beta1 client. Please migrate to the v2 client,
  which is now out of beta.
  - Add v2 client.

- firestore:
  - BEHAVIOR CHANGE: Treat set({}, MergeAll) as valid.

- iam:
  - Support JWT signing via SignJwt callopt.

- profiler:
  - BEHAVIOR CHANGE: PollForSerialOutput returns an error when context.Done.
  - BEHAVIOR CHANGE: Increase the initial backoff to 1 minute.
  - Avoid returning empty serial port output.

- pubsub:
  - BEHAVIOR CHANGE: Don't backoff during next retryable error once stream is healthy.
  - BEHAVIOR CHANGE: Don't backoff on EOF.
  - pstest: Support Acknowledge and ModifyAckDeadline RPCs.

- redis:
  - Add v1 beta Redis client.

- spanner:
  - Support SessionLabels.

- speech:
  - Add api v1 beta1 client.

- storage:
  - BEHAVIOR CHANGE: Retry reads when retryable error occurs.
  - Fix delete of object in requester-pays bucket.
  - Support KMS integration.

## v0.21.0

- bigquery:
  - Add OpenCensus tracing.

- firestore:
  - **BREAKING CHANGE:** If a document does not exist, return a DocumentSnapshot
    whose Exists method returns false. DocumentRef.Get and Transaction.Get
    return the non-nil DocumentSnapshot in addition to a NotFound error.
    **DocumentRef.GetAll and Transaction.GetAll return a non-nil
    DocumentSnapshot instead of nil.**
  - Add DocumentIterator.Stop. **Call Stop whenever you are done with a
    DocumentIterator.**
  - Added Query.Snapshots and DocumentRef.Snapshots, which provide realtime
    notification of updates. See https://cloud.google.com/firestore/docs/query-data/listen.
  - Canceling an RPC now always returns a grpc.Status with codes.Canceled.

- spanner:
  - Add `CommitTimestamp`, which supports inserting the commit timestamp of a
    transaction into a column.

## v0.20.0

- bigquery: Support SchemaUpdateOptions for load jobs.

- bigtable:
  - Add SampleRowKeys.
  - cbt: Support union, intersection GCPolicy.
  - Retry admin RPCS.
  - Add trace spans to retries.

- datastore: Add OpenCensus tracing.

- firestore:
  - Fix queries involving Null and NaN.
  - Allow Timestamp protobuffers for time values.

- logging: Add a WriteTimeout option.

- spanner: Support Batch API.

- storage: Add OpenCensus tracing.

## v0.19.0

- bigquery:
  - Support customer-managed encryption keys.

- bigtable:
  - Improved emulator support.
  - Support GetCluster.

- datastore:
  - Add general mutations.
  - Support pointer struct fields.
  - Support transaction options.

- firestore:
  - Add Transaction.GetAll.
  - Support document cursors.

- logging:
  - Support concurrent RPCs to the service.
  - Support per-entry resources.

- profiler:
  - Add config options to disable heap and thread profiling.
  - Read the project ID from $GOOGLE_CLOUD_PROJECT when it's set.

- pubsub:
  - BEHAVIOR CHANGE: Release flow control after ack/nack (instead of after the
    callback returns).
  - Add SubscriptionInProject.
  - Add OpenCensus instrumentation for streaming pull.

- storage:
  - Support CORS.

## v0.18.0

- bigquery:
  - Marked stable.
  - Schema inference of nullable fields supported.
  - Added TimePartitioning to QueryConfig.

- firestore: Data provided to DocumentRef.Set with a Merge option can contain
  Delete sentinels.

- logging: Clients can accept parent resources other than projects.

- pubsub:
  - pubsub/pstest: A lighweight fake for pubsub. Experimental; feedback welcome.
  - Support updating more subscription metadata: AckDeadline,
    RetainAckedMessages and RetentionDuration.

- oslogin/apiv1beta: New client for the Cloud OS Login API.

- rpcreplay: A package for recording and replaying gRPC traffic.

- spanner:
  - Add a ReadWithOptions that supports a row limit, as well as an index.
  - Support query plan and execution statistics.
  - Added [OpenCensus](http://opencensus.io) support.

- storage: Clarify checksum validation for gzipped files (it is not validated
  when the file is served uncompressed).


## v0.17.0

- firestore BREAKING CHANGES:
  - Remove UpdateMap and UpdateStruct; rename UpdatePaths to Update.
    Change
        `docref.UpdateMap(ctx, map[string]interface{}{"a.b", 1})`
    to
        `docref.Update(ctx, []firestore.Update{{Path: "a.b", Value: 1}})`

    Change
        `docref.UpdateStruct(ctx, []string{"Field"}, aStruct)`
    to
        `docref.Update(ctx, []firestore.Update{{Path: "Field", Value: aStruct.Field}})`
  - Rename MergePaths to Merge; require args to be FieldPaths
  - A value stored as an integer can be read into a floating-point field, and vice versa.
- bigtable/cmd/cbt:
  - Support deleting a column.
  - Add regex option for row read.
- spanner: Mark stable.
- storage:
  - Add Reader.ContentEncoding method.
  - Fix handling of SignedURL headers.
- bigquery:
  - If Uploader.Put is called with no rows, it returns nil without making a
    call.
  - Schema inference supports the "nullable" option in struct tags for
    non-required fields.
  - TimePartitioning supports "Field".


## v0.16.0

- Other bigquery changes:
  - `JobIterator.Next` returns `*Job`; removed `JobInfo` (BREAKING CHANGE).
  - UseStandardSQL is deprecated; set UseLegacySQL to true if you need
    Legacy SQL.
  - Uploader.Put will generate a random insert ID if you do not provide one.
  - Support time partitioning for load jobs.
  - Support dry-run queries.
  - A `Job` remembers its last retrieved status.
  - Support retrieving job configuration.
  - Support labels for jobs and tables.
  - Support dataset access lists.
  - Improve support for external data sources, including data from Bigtable and
    Google Sheets, and tables with external data.
  - Support updating a table's view configuration.
  - Fix uploading civil times with nanoseconds.

- storage:
  - Support PubSub notifications.
  - Support Requester Pays buckets.

- profiler: Support goroutine and mutex profile types.

## v0.15.0

- firestore: beta release. See the
  [announcement](https://firebase.googleblog.com/2017/10/introducing-cloud-firestore.html).

- errorreporting: The existing package has been redesigned.

- errors: This package has been removed. Use errorreporting.


## v0.14.0

- bigquery BREAKING CHANGES:
  - Standard SQL is the default for queries and views.
  - `Table.Create` takes `TableMetadata` as a second argument, instead of
    options.
  - `Dataset.Create` takes `DatasetMetadata` as a second argument.
  - `DatasetMetadata` field `ID` renamed to `FullID`
  - `TableMetadata` field `ID` renamed to `FullID`

- Other bigquery changes:
  - The client will append a random suffix to a provided job ID if you set
    `AddJobIDSuffix` to true in a job config.
  - Listing jobs is supported.
  - Better retry logic.

- vision, language, speech: clients are now stable

- monitoring: client is now beta

- profiler:
  - Rename InstanceName to Instance, ZoneName to Zone
  - Auto-detect service name and version on AppEngine.

## v0.13.0

- bigquery: UseLegacySQL options for CreateTable and QueryConfig. Use these
  options to continue using Legacy SQL after the client switches its default
  to Standard SQL.

- bigquery: Support for updating dataset labels.

- bigquery: Set DatasetIterator.ProjectID to list datasets in a project other
  than the client's. DatasetsInProject is no longer needed and is deprecated.

- bigtable: Fail ListInstances when any zones fail.

- spanner: support decoding of slices of basic types (e.g. []string, []int64,
  etc.)

- logging/logadmin: UpdateSink no longer creates a sink if it is missing
  (actually a change to the underlying service, not the client)

- profiler: Service and ServiceVersion replace Target in Config.

## v0.12.0

- pubsub: Subscription.Receive now uses streaming pull.

- pubsub: add Client.TopicInProject to access topics in a different project
  than the client.

- errors: renamed errorreporting. The errors package will be removed shortly.

- datastore: improved retry behavior.

- bigquery: support updates to dataset metadata, with etags.

- bigquery: add etag support to Table.Update (BREAKING: etag argument added).

- bigquery: generate all job IDs on the client.

- storage: support bucket lifecycle configurations.


## v0.11.0

- Clients for spanner, pubsub and video are now in beta.

- New client for DLP.

- spanner: performance and testing improvements.

- storage: requester-pays buckets are supported.

- storage, profiler, bigtable, bigquery: bug fixes and other minor improvements.

- pubsub: bug fixes and other minor improvements

## v0.10.0

- pubsub: Subscription.ModifyPushConfig replaced with Subscription.Update.

- pubsub: Subscription.Receive now runs concurrently for higher throughput.

- vision: cloud.google.com/go/vision is deprecated. Use
cloud.google.com/go/vision/apiv1 instead.

- translation: now stable.

- trace: several changes to the surface. See the link below.

### Code changes required from v0.9.0

- pubsub: Replace

    ```
    sub.ModifyPushConfig(ctx, pubsub.PushConfig{Endpoint: "https://example.com/push"})
    ```

  with

    ```
    sub.Update(ctx, pubsub.SubscriptionConfigToUpdate{
        PushConfig: &pubsub.PushConfig{Endpoint: "https://example.com/push"},
    })
    ```

- trace: traceGRPCServerInterceptor will be provided from *trace.Client.
Given an initialized `*trace.Client` named `tc`, instead of

    ```
    s := grpc.NewServer(grpc.UnaryInterceptor(trace.GRPCServerInterceptor(tc)))
    ```

  write

    ```
    s := grpc.NewServer(grpc.UnaryInterceptor(tc.GRPCServerInterceptor()))
    ```

- trace trace.GRPCClientInterceptor will also provided from *trace.Client.
Instead of

    ```
    conn, err := grpc.Dial(srv.Addr, grpc.WithUnaryInterceptor(trace.GRPCClientInterceptor()))
    ```

  write

    ```
    conn, err := grpc.Dial(srv.Addr, grpc.WithUnaryInterceptor(tc.GRPCClientInterceptor()))
    ```

- trace: We removed the deprecated `trace.EnableGRPCTracing`. Use the gRPC
interceptor as a dial option as shown below when initializing Cloud package
clients:

    ```
    c, err := pubsub.NewClient(ctx, "project-id", option.WithGRPCDialOption(grpc.WithUnaryInterceptor(tc.GRPCClientInterceptor())))
    if err != nil {
        ...
    }
    ```


## v0.9.0

- Breaking changes to some autogenerated clients.
- rpcreplay package added.

## v0.8.0

- profiler package added.
- storage:
  - Retry Objects.Insert call.
  - Add ProgressFunc to WRiter.
- pubsub: breaking changes:
  - Publish is now asynchronous ([announcement](https://groups.google.com/d/topic/google-api-go-announce/aaqRDIQ3rvU/discussion)).
  - Subscription.Pull replaced by Subscription.Receive, which takes a callback ([announcement](https://groups.google.com/d/topic/google-api-go-announce/8pt6oetAdKc/discussion)).
  - Message.Done replaced with Message.Ack and Message.Nack.

## v0.7.0

- Release of a client library for Spanner. See
the
[blog
post](https://cloudplatform.googleblog.com/2017/02/introducing-Cloud-Spanner-a-global-database-service-for-mission-critical-applications.html).
Note that although the Spanner service is beta, the Go client library is alpha.

## v0.6.0

- Beta release of BigQuery, DataStore, Logging and Storage. See the
[blog post](https://cloudplatform.googleblog.com/2016/12/announcing-new-google-cloud-client.html).

- bigquery:
  - struct support. Read a row directly into a struct with
`RowIterator.Next`, and upload a row directly from a struct with `Uploader.Put`.
You can also use field tags. See the [package documentation][cloud-bigquery-ref]
for details.

  - The `ValueList` type was removed. It is no longer necessary. Instead of
   ```go
   var v ValueList
   ... it.Next(&v) ..
   ```
   use

   ```go
   var v []Value
   ... it.Next(&v) ...
   ```

  - Previously, repeatedly calling `RowIterator.Next` on the same `[]Value` or
  `ValueList` would append to the slice. Now each call resets the size to zero first.

  - Schema inference will infer the SQL type BYTES for a struct field of
  type []byte. Previously it inferred STRING.

  - The types `uint`, `uint64` and `uintptr` are no longer supported in schema
  inference. BigQuery's integer type is INT64, and those types may hold values
  that are not correctly represented in a 64-bit signed integer.

## v0.5.0

- bigquery:
  - The SQL types DATE, TIME and DATETIME are now supported. They correspond to
    the `Date`, `Time` and `DateTime` types in the new `cloud.google.com/go/civil`
    package.
  - Support for query parameters.
  - Support deleting a dataset.
  - Values from INTEGER columns will now be returned as int64, not int. This
    will avoid errors arising from large values on 32-bit systems.
- datastore:
  - Nested Go structs encoded as Entity values, instead of a
flattened list of the embedded struct's fields. This means that you may now have twice-nested slices, eg.
    ```go
    type State struct {
      Cities  []struct{
        Populations []int
      }
    }
    ```
    See [the announcement](https://groups.google.com/forum/#!topic/google-api-go-announce/79jtrdeuJAg) for
more details.
  - Contexts no longer hold namespaces; instead you must set a key's namespace
    explicitly. Also, key functions have been changed and renamed.
  - The WithNamespace function has been removed. To specify a namespace in a Query, use the Query.Namespace method:
     ```go
     q := datastore.NewQuery("Kind").Namespace("ns")
     ```
  - All the fields of Key are exported. That means you can construct any Key with a struct literal:
     ```go
     k := &Key{Kind: "Kind",  ID: 37, Namespace: "ns"}
     ```
  - As a result of the above, the Key methods Kind, ID, d.Name, Parent, SetParent and Namespace have been removed.
  - `NewIncompleteKey` has been removed, replaced by `IncompleteKey`. Replace
      ```go
      NewIncompleteKey(ctx, kind, parent)
      ```
      with
      ```go
      IncompleteKey(kind, parent)
      ```
      and if you do use namespaces, make sure you set the namespace on the returned key.
  - `NewKey` has been removed, replaced by `NameKey` and `IDKey`. Replace
      ```go
      NewKey(ctx, kind, name, 0, parent)
      NewKey(ctx, kind, "", id, parent)
      ```
      with
      ```go
      NameKey(kind, name, parent)
      IDKey(kind, id, parent)
      ```
      and if you do use namespaces, make sure you set the namespace on the returned key.
  - The `Done` variable has been removed. Replace `datastore.Done` with `iterator.Done`, from the package `google.golang.org/api/iterator`.
  - The `Client.Close` method will have a return type of error. It will return the result of closing the underlying gRPC connection.
  - See [the announcement](https://groups.google.com/forum/#!topic/google-api-go-announce/hqXtM_4Ix-0) for
more details.

## v0.4.0

- bigquery:
  -`NewGCSReference` is now a function, not a method on `Client`.
  - `Table.LoaderFrom` now accepts a `ReaderSource`, enabling
     loading data into a table from a file or any `io.Reader`.
  * Client.Table and Client.OpenTable have been removed.
      Replace
      ```go
      client.OpenTable("project", "dataset", "table")
      ```
      with
      ```go
      client.DatasetInProject("project", "dataset").Table("table")
      ```

  * Client.CreateTable has been removed.
      Replace
      ```go
      client.CreateTable(ctx, "project", "dataset", "table")
      ```
      with
      ```go
      client.DatasetInProject("project", "dataset").Table("table").Create(ctx)
      ```

  * Dataset.ListTables have been replaced with Dataset.Tables.
      Replace
      ```go
      tables, err := ds.ListTables(ctx)
      ```
      with
      ```go
      it := ds.Tables(ctx)
      for {
          table, err := it.Next()
          if err == iterator.Done {
              break
          }
          if err != nil {
              // TODO: Handle error.
          }
          // TODO: use table.
      }
      ```

  * Client.Read has been replaced with Job.Read, Table.Read and Query.Read.
      Replace
      ```go
      it, err := client.Read(ctx, job)
      ```
      with
      ```go
      it, err := job.Read(ctx)
      ```
    and similarly for reading from tables or queries.

  * The iterator returned from the Read methods is now named RowIterator. Its
    behavior is closer to the other iterators in these libraries. It no longer
    supports the Schema method; see the next item.
      Replace
      ```go
      for it.Next(ctx) {
          var vals ValueList
          if err := it.Get(&vals); err != nil {
              // TODO: Handle error.
          }
          // TODO: use vals.
      }
      if err := it.Err(); err != nil {
          // TODO: Handle error.
      }
      ```
      with
      ```
      for {
          var vals ValueList
          err := it.Next(&vals)
          if err == iterator.Done {
              break
          }
          if err != nil {
              // TODO: Handle error.
          }
          // TODO: use vals.
      }
      ```
      Instead of the `RecordsPerRequest(n)` option, write
      ```go
      it.PageInfo().MaxSize = n
      ```
      Instead of the `StartIndex(i)` option, write
      ```go
      it.StartIndex = i
      ```

  * ValueLoader.Load now takes a Schema in addition to a slice of Values.
      Replace
      ```go
      func (vl *myValueLoader) Load(v []bigquery.Value)
      ```
      with
      ```go
      func (vl *myValueLoader) Load(v []bigquery.Value, s bigquery.Schema)
      ```


  * Table.Patch is replace by Table.Update.
      Replace
      ```go
      p := table.Patch()
      p.Description("new description")
      metadata, err := p.Apply(ctx)
      ```
      with
      ```go
      metadata, err := table.Update(ctx, bigquery.TableMetadataToUpdate{
          Description: "new description",
      })
      ```

  * Client.Copy is replaced by separate methods for each of its four functions.
    All options have been replaced by struct fields.

    * To load data from Google Cloud Storage into a table, use Table.LoaderFrom.

      Replace
      ```go
      client.Copy(ctx, table, gcsRef)
      ```
      with
      ```go
      table.LoaderFrom(gcsRef).Run(ctx)
      ```
      Instead of passing options to Copy, set fields on the Loader:
      ```go
      loader := table.LoaderFrom(gcsRef)
      loader.WriteDisposition = bigquery.WriteTruncate
      ```

    * To extract data from a table into Google Cloud Storage, use
      Table.ExtractorTo. Set fields on the returned Extractor instead of
      passing options.

      Replace
      ```go
      client.Copy(ctx, gcsRef, table)
      ```
      with
      ```go
      table.ExtractorTo(gcsRef).Run(ctx)
      ```

    * To copy data into a table from one or more other tables, use
      Table.CopierFrom. Set fields on the returned Copier instead of passing options.

      Replace
      ```go
      client.Copy(ctx, dstTable, srcTable)
      ```
      with
      ```go
      dst.Table.CopierFrom(srcTable).Run(ctx)
      ```

    * To start a query job, create a Query and call its Run method. Set fields
    on the query instead of passing options.

      Replace
      ```go
      client.Copy(ctx, table, query)
      ```
      with
      ```go
      query.Run(ctx)
      ```

  * Table.NewUploader has been renamed to Table.Uploader. Instead of options,
    configure an Uploader by setting its fields.
      Replace
      ```go
      u := table.NewUploader(bigquery.UploadIgnoreUnknownValues())
      ```
      with
      ```go
      u := table.NewUploader(bigquery.UploadIgnoreUnknownValues())
      u.IgnoreUnknownValues = true
      ```

- pubsub: remove `pubsub.Done`. Use `iterator.Done` instead, where `iterator` is the package
`google.golang.org/api/iterator`.

## v0.3.0

- storage:
  * AdminClient replaced by methods on Client.
      Replace
      ```go
      adminClient.CreateBucket(ctx, bucketName, attrs)
      ```
      with
      ```go
      client.Bucket(bucketName).Create(ctx, projectID, attrs)
      ```

  * BucketHandle.List replaced by BucketHandle.Objects.
      Replace
      ```go
      for query != nil {
          objs, err := bucket.List(d.ctx, query)
          if err != nil { ... }
          query = objs.Next
          for _, obj := range objs.Results {
              fmt.Println(obj)
          }
      }
      ```
      with
      ```go
      iter := bucket.Objects(d.ctx, query)
      for {
          obj, err := iter.Next()
          if err == iterator.Done {
              break
          }
          if err != nil { ... }
          fmt.Println(obj)
      }
      ```
      (The `iterator` package is at `google.golang.org/api/iterator`.)

      Replace `Query.Cursor` with `ObjectIterator.PageInfo().Token`.

      Replace `Query.MaxResults` with `ObjectIterator.PageInfo().MaxSize`.


  * ObjectHandle.CopyTo replaced by ObjectHandle.CopierFrom.
      Replace
      ```go
      attrs, err := src.CopyTo(ctx, dst, nil)
      ```
      with
      ```go
      attrs, err := dst.CopierFrom(src).Run(ctx)
      ```

      Replace
      ```go
      attrs, err := src.CopyTo(ctx, dst, &storage.ObjectAttrs{ContextType: "text/html"})
      ```
      with
      ```go
      c := dst.CopierFrom(src)
      c.ContextType = "text/html"
      attrs, err := c.Run(ctx)
      ```

  * ObjectHandle.ComposeFrom replaced by ObjectHandle.ComposerFrom.
      Replace
      ```go
      attrs, err := dst.ComposeFrom(ctx, []*storage.ObjectHandle{src1, src2}, nil)
      ```
      with
      ```go
      attrs, err := dst.ComposerFrom(src1, src2).Run(ctx)
      ```

  * ObjectHandle.Update's ObjectAttrs argument replaced by ObjectAttrsToUpdate.
      Replace
      ```go
      attrs, err := obj.Update(ctx, &storage.ObjectAttrs{ContextType: "text/html"})
      ```
      with
      ```go
      attrs, err := obj.Update(ctx, storage.ObjectAttrsToUpdate{ContextType: "text/html"})
      ```

  * ObjectHandle.WithConditions replaced by ObjectHandle.If.
      Replace
      ```go
      obj.WithConditions(storage.Generation(gen), storage.IfMetaGenerationMatch(mgen))
      ```
      with
      ```go
      obj.Generation(gen).If(storage.Conditions{MetagenerationMatch: mgen})
      ```

      Replace
      ```go
      obj.WithConditions(storage.IfGenerationMatch(0))
      ```
      with
      ```go
      obj.If(storage.Conditions{DoesNotExist: true})
      ```

  * `storage.Done` replaced by `iterator.Done` (from package `google.golang.org/api/iterator`).

- Package preview/logging deleted. Use logging instead.

## v0.2.0

- Logging client replaced with preview version (see below).

- New clients for some of Google's Machine Learning APIs: Vision, Speech, and
Natural Language.

- Preview version of a new [Stackdriver Logging][cloud-logging] client in
[`cloud.google.com/go/preview/logging`](https://godoc.org/cloud.google.com/go/preview/logging).
This client uses gRPC as its transport layer, and supports log reading, sinks
and metrics. It will replace the current client at `cloud.google.com/go/logging` shortly.


