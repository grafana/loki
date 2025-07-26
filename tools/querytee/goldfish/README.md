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
- **Query Engine Version Tracking**: Tracks which queries used the new experimental query engine vs the old engine
- **Persistent Storage**: MySQL storage via Google Cloud SQL Proxy or Amazon RDS for storing query samples and comparison results

## Configuration

Goldfish is configured through command-line flags:

```bash
# Enable Goldfish
-goldfish.enabled=true

# Sampling configuration
-goldfish.sampling.default-rate=0.1                              # Sample 10% of queries by default
-goldfish.sampling.tenant-rules="tenant1:0.5,tenant2:1.0"       # Tenant-specific rates

# Storage configuration (optional - defaults to no-op if not specified)

# Option 1: CloudSQL (MySQL) configuration
-goldfish.storage.type=cloudsql
-goldfish.storage.cloudsql.host=cloudsql-proxy                  # CloudSQL proxy host
-goldfish.storage.cloudsql.port=3306                           # MySQL port (default: 3306)
-goldfish.storage.cloudsql.database=goldfish
-goldfish.storage.cloudsql.user=goldfish-user

# Option 2: RDS (MySQL) configuration
-goldfish.storage.type=rds
-goldfish.storage.rds.endpoint=mydb.123456789012.us-east-1.rds.amazonaws.com:3306
-goldfish.storage.rds.database=goldfish
-goldfish.storage.rds.user=goldfish-user

# Password must be provided via GOLDFISH_DB_PASSWORD environment variable (for both CloudSQL and RDS)
export GOLDFISH_DB_PASSWORD=your-password

# Connection pool settings (apply to both CloudSQL and RDS)
-goldfish.storage.max-connections=10                            # Maximum database connections
-goldfish.storage.max-idle-time=300                            # Connection idle timeout (seconds)

# Performance comparison settings
-goldfish.performance-tolerance=0.1                             # 10% tolerance for execution time variance

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
- Query engine version tracking:
  - cell_a_used_new_engine (BOOLEAN)
  - cell_b_used_new_engine (BOOLEAN)
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
   - Execution time variance (with configurable tolerance for normal variation)
   - Bytes/lines processed (must be identical for same query)
   - Query complexity differences (splits, shards)

3. **Status Code Comparison**:
   - Different status codes = **MISMATCH**
   - Both non-200 status codes = **MATCH** (both failed consistently)

4. **Query Engine Version Detection**:
   - Goldfish detects when queries use the new experimental query engine by parsing warnings in the response
   - When Loki includes the warning "Query was executed using the new experimental query engine and dataobj storage.", Goldfish tracks this in the database
   - This helps identify which queries are using the new vs old engine during migration

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

-- Query engine version analysis
SELECT
  sq.tenant_id,
  SUM(CASE WHEN sq.cell_a_used_new_engine THEN 1 ELSE 0 END) as cell_a_new_engine_count,
  SUM(CASE WHEN sq.cell_b_used_new_engine THEN 1 ELSE 0 END) as cell_b_new_engine_count,
  COUNT(*) as total_queries
FROM sampled_queries sq
GROUP BY sq.tenant_id;

-- Find queries where cells used different engines
SELECT * FROM sampled_queries
WHERE cell_a_used_new_engine != cell_b_used_new_engine;
```

## Storage Configuration

Goldfish supports MySQL storage via Google Cloud SQL Proxy or Amazon RDS. The storage is optional - if not configured, Goldfish will perform sampling and comparison but won't persist results.

### Setting up CloudSQL

1. Ensure your CloudSQL proxy is running and accessible
2. Create a MySQL database for Goldfish
3. Configure the connection parameters via flags
4. Set the database password using the `GOLDFISH_DB_PASSWORD` environment variable

### Setting up RDS

1. Create an RDS MySQL instance
2. Ensure your Loki cells can reach the RDS endpoint
3. Create a MySQL database for Goldfish
4. Configure the connection parameters via flags
5. Set the database password using the `GOLDFISH_DB_PASSWORD` environment variable

The schema will be automatically created on first run for both CloudSQL and RDS.

## Testing

The Goldfish implementation includes comprehensive tests that verify:

- Hash-based comparison logic
- Performance statistics analysis
- Status code handling
- SQL injection protection with parameterized queries
- Configurable performance tolerance
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
