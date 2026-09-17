# dataobj-locality

`dataobj-locality` measures how well label values and log records are
physically clustered inside Loki's data objects. It reads directly from the
live index (postings sections) and, optionally, the logs objects referenced by
that index, for a given tenant and time range.

Because scanning hundreds of index objects is expensive, the tool supports
exporting raw posting facts as Parquet and CSV so the data can be shared
and re-queried offline without re-scanning.

## Installation

```bash
go build -o dataobj-locality ./tools/dataobj-locality/
```

## Usage

```
dataobj-locality [flags]
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--config.file` | | Path to a Loki config YAML (reads storage credentials and prefixes from it, same as the running engine). Mutually exclusive with `--dir`. |
| `--config.expand-env` | false | Expand `${VAR}` in the config file before parsing. |
| `--dir` | | Local directory holding TOC and index objects. Useful for local testing. Mutually exclusive with `--config.file`. |
| `--tenant` | | Tenant ID to inspect. Required for bucket sources. |
| `--from` | now - 12h | Start of the time range (RFC3339). |
| `--to` | now | End of the time range (RFC3339). |
| `--locality` | both | What to measure: `postings`, `logs`, or `both`. |
| `--sort-key` | service_name | Label used as the primary sort dimension for the logs locality rollup. |
| `--logs-section-target-bytes` | 134217728 (128 MiB) | Uncompressed logs-section target size, used to compute the ideal section count. |
| `--top` | 20 | Rows to show in each top-N table. |
| `--concurrency` | 8 | Maximum number of index objects to open and scan in parallel. |
| `--export` | | Path prefix for raw-fact export. Produces `<prefix>.parquet` and/or `<prefix>.csv`. |
| `--export-format` | both | Output format for `--export`: `parquet`, `csv`, or `both`. |

## Examples

### Quick console report against a live bucket

```bash
dataobj-locality \
  --config.file /etc/loki/config.yaml \
  --tenant acme \
  --from 2024-01-15T00:00:00Z \
  --to   2024-01-15T06:00:00Z \
  --locality both \
  --sort-key service_name
```

This prints a summary and top-20 tables to stdout, then exits.

### Credentials

The tool reuses Loki's own storage configuration, so credentials are resolved
through the same chain the engine uses:

- **AWS S3**: set `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` in the
  environment, or rely on instance-profile / workload-identity if running
  inside AWS.
- **GCS**: set `GOOGLE_APPLICATION_CREDENTIALS`, or rely on Application
  Default Credentials (`gcloud auth application-default login` for local
  development).
- **Local testing**: use `--dir` pointing at a directory that mirrors the
  object-store layout.

### Export to Parquet + CSV, then query with DuckDB

```bash
dataobj-locality \
  --config.file /etc/loki/config.yaml \
  --tenant acme \
  --from 2024-01-15T00:00:00Z \
  --to   2024-01-15T06:00:00Z \
  --locality both \
  --export ./facts \
  --export-format both
```

This produces `facts.parquet` and `facts.csv` alongside the console report.

## Querying with DuckDB

Install DuckDB: <https://duckdb.org/docs/installation>

```bash
duckdb
```

### Schema

| Column | Type | Description |
|---|---|---|
| `tenant` | VARCHAR | Tenant the posting belongs to. |
| `index_object` | VARCHAR | Path of the index object that contains the postings section. |
| `index_section` | BIGINT | Section index of the postings section within the index object. Together with `index_object` this uniquely identifies a physical postings section, matching the `sectionSpread` metric in the console report. |
| `compacted` | BOOLEAN | Whether the index object is a compacted (IndexMerge) output rather than an uncompacted per-partition index. Derived from the index object path (`indexes/tenants/...` = compacted). |
| `column_name` | VARCHAR | Label name (e.g. `service_name`). |
| `label_value` | VARCHAR | Label value (e.g. `frontend`). |
| `logs_object` | VARCHAR | Path of the logs object referenced by the posting. |
| `logs_section` | BIGINT | Section index within the logs object. |
| `stream_refs` | BIGINT | Number of distinct log streams in the section for this label value. |
| `uncompressed_size` | BIGINT | Uncompressed bytes of log records for this label value in this section. |

### Useful queries

**How many distinct logs sections does each service touch?**

```sql
SELECT
    label_value                          AS service,
    COUNT(DISTINCT (logs_object, logs_section)) AS logs_sections,
    COUNT(DISTINCT logs_object)          AS logs_objects,
    SUM(uncompressed_size) / 1024 / 1024 AS total_mb
FROM 'facts.parquet'
WHERE column_name = 'service_name'
GROUP BY label_value
ORDER BY logs_sections DESC
LIMIT 20;
```

**Spread factor: actual sections vs ideal (128 MiB target)**

```sql
SELECT
    label_value AS service,
    COUNT(DISTINCT (logs_object, logs_section)) AS actual_sections,
    CEIL(SUM(uncompressed_size) / 134217728.0) AS ideal_sections,
    ROUND(
        COUNT(DISTINCT (logs_object, logs_section)) /
        CEIL(SUM(uncompressed_size) / 134217728.0),
        2
    ) AS spread_factor
FROM 'facts.parquet'
WHERE column_name = 'service_name'
GROUP BY label_value
HAVING ideal_sections >= 1
ORDER BY spread_factor DESC
LIMIT 20;
```

A `spread_factor` of 1.0 means logs are perfectly clustered. Higher values
indicate fragmentation — the service's logs are spread across more sections
than the data volume requires.

**Postings-section spread per label name (how many index sections reference each label?)**

```sql
SELECT
    column_name,
    COUNT(DISTINCT index_object)                  AS index_objects,
    COUNT(DISTINCT (index_object, index_section)) AS postings_sections,
    SUM(stream_refs)                              AS total_stream_refs
FROM 'facts.parquet'
GROUP BY column_name
ORDER BY postings_sections DESC
LIMIT 20;
```

**Re-slice by a different sort key without re-scanning**

Because the export contains every label column, you can analyse locality for
any label — not just the one used as `--sort-key` at scan time:

```sql
SELECT
    label_value AS namespace,
    COUNT(DISTINCT (logs_object, logs_section)) AS logs_sections,
    CEIL(SUM(uncompressed_size) / 134217728.0) AS ideal_sections
FROM 'facts.parquet'
WHERE column_name = 'namespace'
GROUP BY label_value
ORDER BY logs_sections DESC
LIMIT 20;
```
