# Loki Index Auditing

## Usage

To audit your index data:
1. Make sure you're authenticated to the cloud where your bucket lives in.
In this example I'll be using GCP.
2. Create a new YAML configuration file that defines your storage configuration.
`lokitool` will use it to communicate with your data.
Only TSDB is supported. Make sure you give all three fields: `schema_config`, `storage_config` and `tenant`. In this example I'm naming my file `configfile.yaml`:
```yaml
schema_config:
  configs:
    - from: "2023-08-21"
      index:
        period: 24h
        prefix: loki_env_tsdb_index_
      object_store: gcs
      schema: v13
      store: tsdb

storage_config:
  gcs:
    bucket_name: loki-bucket

tenant: 12345
```
3. Build a new `lokitool` binary:
```bash
go build ./cmd/lokitool
```
4. Finally, invoke the `audit index` command the following way:
```bash
./lokitool audit index --period=19856 --config.file=configfile.yaml --index.file=index/loki_env_tsdb_index_19856/12345/1715707992714992001-compactor-1715199977885-1815707796275-g8003361.tsdb.gz
```
The `--period` is the period of the index being audited. You can find it by checking the 5-digits number appended
as a suffix of the Loki environment name in the index file. Example: For `index/loki_env_tsdb_index_19856/12345/...`,
the period is 19856.
The `--config.file` is the YAML configuration described in the first step.
The `--index.file` is the path to the index file you want to audit. Take a look at your bucket to see its exactly path and substitute it accordingly.