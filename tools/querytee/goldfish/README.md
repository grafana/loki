# Goldfish - Query Comparison for Loki QueryTee

**⚠️ EXPERIMENTAL**: Goldfish is an experimental feature and its API/configuration may change in future releases.

Goldfish is a feature within QueryTee that enables sampling and comparison of query responses between multiple Loki cells. It helps identify discrepancies and performance differences between cells during migrations or when running multiple Loki deployments.

## Features

- **Tenant-based Sampling**: Configure sampling rates per tenant or use a default rate
- **Privacy-Compliant Comparison**: Hash-based comparison without storing sensitive data:
  - Response integrity verification using fnv32 content hashes
  - Performance statistics comparison and analysis
- **Performance Analysis**: Rich performance metrics tracking:
  - Execution time, queue time, processing rates
  - Bytes/lines processed comparison
  - Query complexity metrics (splits, shards)
  - Performance variance detection and reporting
- **Flexible Storage**: Support for multiple database backends:
  - Google Cloud SQL
  - BigQuery
  - Generic SQL databases (PostgreSQL, MySQL)

## Configuration

Goldfish is configured through command-line flags:

```bash
# Enable Goldfish
-goldfish.enabled=true

# Sampling configuration
-goldfish.sampling.default-rate=0.1                              # Sample 10% of queries by default
-goldfish.sampling.tenant-rules="tenant1:0.5,tenant2:1.0"       # Tenant-specific rates

# Storage configuration (optional - defaults to no-op if not specified)
# CloudSQL example:
-goldfish.storage.type=cloudsql
-goldfish.storage.cloudsql.host=cloudsql-proxy                  # CloudSQL proxy host
-goldfish.storage.cloudsql.port=5432                           # CloudSQL proxy port
-goldfish.storage.cloudsql.database=goldfish
-goldfish.storage.cloudsql.user=goldfish-user
-goldfish.storage.cloudsql.password=your-password
-goldfish.storage.max-connections=10                            # Maximum database connections
-goldfish.storage.max-idle-time=300                            # Connection idle timeout (seconds)

# Or run without storage (sampling and comparison only, no persistence)
# Simply omit the storage configuration
```

## Architecture

```
┌─────────────┐     ┌─────────────┐
│   Client    │────▶│  QueryTee   │ ◄── Existing functionality unchanged
└─────────────┘     └──────┬──────┘
                           │
                    ┌──────┴──────┐ ◄── Optional Goldfish integration
                    │  Goldfish   │     (only when enabled)
                    │   Manager   │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Sampler  │ │Hash-based│ │ Storage  │
        │          │ │Comparator│ │          │
        └──────────┘ └──────────┘ └──────────┘
```

## Database Schema

Goldfish uses two main tables:

### sampled_queries
Stores query metadata and performance statistics (no sensitive data):
- correlation_id (PRIMARY KEY)
- tenant_id, query, query_type
- start_time, end_time, step_duration
- Performance statistics for both cells:
  - exec_time_ms, queue_time_ms
  - bytes_processed, lines_processed
  - bytes_per_second, lines_per_second
  - entries_returned, splits, shards
- Response metadata without content:
  - response_hash (for integrity verification)
  - response_size, status_code
- sampled_at

### comparison_outcomes
Stores the results of comparing responses:
- correlation_id (PRIMARY KEY, FOREIGN KEY)
- comparison_status (match, mismatch, error, partial)
- difference_details (JSONB)
- performance_metrics (JSONB)
- compared_at

## Comparison Logic

Goldfish uses a simplified, privacy-focused comparison approach:

1. **Content Integrity**: Response content is hashed (fnv32) for integrity verification
   - Matching hashes = identical content = **MATCH**
   - Different hashes = different content = **MISMATCH**

2. **Performance Analysis**: Execution statistics are compared for optimization insights:
   - Execution time variance (with 10% tolerance for normal variation)
   - Bytes/lines processed (must be identical for same query)
   - Query complexity differences (splits, shards)

3. **Status Code Comparison**:
   - Different status codes = **MISMATCH**
   - Both non-200 status codes = **MATCH** (both failed consistently)

**Important**: Performance differences do NOT affect match status. If content hashes match, queries are considered equivalent regardless of execution time differences.

## Usage

Once configured, Goldfish automatically:

1. Samples queries based on tenant configuration
2. Captures responses from both Loki cells
3. Extracts performance statistics and computes content hashes
4. Compares hashes and performance metrics
5. Stores results in the configured database

Query the database to analyze differences:

```sql
-- Find all mismatches for a tenant
SELECT * FROM comparison_outcomes co
JOIN sampled_queries sq ON co.correlation_id = sq.correlation_id
WHERE sq.tenant_id = 'tenant1'
  AND co.comparison_status = 'mismatch';

-- Performance comparison
SELECT
  sq.tenant_id,
  AVG((co.performance_metrics->>'QueryTimeRatio')::float) as avg_time_ratio,
  COUNT(*) as query_count
FROM comparison_outcomes co
JOIN sampled_queries sq ON co.correlation_id = sq.correlation_id
GROUP BY sq.tenant_id;
```

## Development

To add a new storage backend:

1. Implement the `Storage` interface in a new file (e.g., `storage_bigquery.go`)
2. Add the backend type to the `NewStorage` factory function
3. Add any backend-specific configuration to `StorageConfig`
4. Add backend-specific flags to `RegisterFlags`

## Testing

The Goldfish implementation includes comprehensive tests that verify:
- Hash-based comparison logic
- Performance statistics analysis
- Status code handling
- Backward compatibility with existing QueryTee functionality

Run tests with:

```bash
# Test only Goldfish functionality
go test ./tools/querytee/goldfish/...

# Test QueryTee including Goldfish integration
go test ./tools/querytee/...

# Build the QueryTee binary with Goldfish support
make loki-querytee
```
