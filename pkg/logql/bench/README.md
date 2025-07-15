# LogQL Benchmark Suite

This directory contains a comprehensive benchmark suite for LogQL, Loki's query language. The suite is designed to generate realistic log data and benchmark various LogQL queries against different storage implementations.

## Overview

The LogQL benchmark suite provides tools to:

1. Generate realistic log data with configurable cardinality, patterns, and time distributions
2. Store the generated data in different storage formats (currently supports "chunk" and "dataobj" formats)
3. Run benchmarks against a variety of LogQL queries, including log filtering and metric queries
4. Compare performance between different storage implementations

## Getting Started

### Prerequisites

- Go 1.21 or later
- At least 2GB of free disk space (for default dataset size)

### Generating Test Data

Before running benchmarks, you need to generate test data:

```bash
# Generate default dataset (2GB)
make generate

# Generate a custom-sized dataset (e.g., 500MB)
make generate SIZE=524288000

# Generate for a specific tenant
make generate TENANT=my-tenant
```

The data generation process:

1. Creates synthetic log data with realistic patterns
2. Stores the data in multiple storage formats for comparison
3. Saves a configuration file that describes the generated dataset

The generated dataset is fully reproducible as it uses a fixed random seed. This ensures that benchmark results are comparable across different runs and environments, making it ideal for performance regression testing.

### Running Benchmarks

Once data is generated, you can run benchmarks:

```bash
# Run all benchmarks
make bench

# List available benchmark queries
make list

# Run benchmarks with interactive UI
make run

# Run with debug output, you need to tail the logs to see the output `tail -f debug.log`
make run-debug

# Stream sample logs from the dataset
make stream
```

## Architecture

### Data Generation

The benchmark suite generates synthetic log data with:

- Configurable label cardinality (clusters, namespaces, services, pods, containers)
- Realistic log patterns for different applications (nginx, postgres, java, etc.)
- Time-based patterns including dense intervals with higher log volume
- Structured metadata and trace context

### Storage Implementations

The suite supports multiple storage implementations:

1. **DataObj Store**: Stores logs as serialized protocol buffer objects
2. **Chunk Store**: Stores logs in compressed chunks similar to Loki's chunk format

### Query Types

The benchmark includes various query types:

- Log filtering queries (e.g., `{app="nginx"} |= "error"`)
- Metric queries (e.g., `rate({app="nginx"}[5m])`)
- Aggregations (e.g., `sum by (status_code) (rate({app="nginx"} | json | status_code != "" [5m]))`)

## Extending the Suite

### Adding New Application Types

The benchmark suite supports multiple application types, each generating different log formats. Currently, it includes applications like web servers, databases, caches, Nginx, Kubernetes, Prometheus, Grafana, Loki, and more.

To add a new application type:

1. Open `faker.go` and add any new data variables needed for your application:

   ```go
   myAppComponents := []string{
       "component1",
       "component2",
       // ...
   }
   ```

2. Add helper methods to the `Faker` struct if needed:

   ```go
   // MyAppComponent returns a random component for my application
   func (f *Faker) MyAppComponent() string {
       return myAppComponents[f.rnd.Intn(len(myAppComponents))]
   }
   ```

3. Add a new entry to the `defaultApplications` slice:

   ```go
   {
       Name: "my-application",
       LogGenerator: func(level string, ts time.Time, f *Faker) string {
           // Generate log line in your desired format (JSON, logfmt, etc.)
           return fmt.Sprintf(
               `level=%s ts=%s component=%s msg="My application log"`,
               level, ts.Format(time.RFC3339), f.MyAppComponent(),
           )
       },
       OTELResource: map[string]string{
           "service_name":    "my-application",
           "service_version": "1.0.0",
           // Add any OpenTelemetry resource attributes
       },
   }
   ```

4. The `LogGenerator` function receives:
   - `level`: The log level (info, warn, error, etc.)
   - `ts`: The timestamp for the log entry
   - `f`: The faker instance for generating random data

5. The `OTELResource` map defines OpenTelemetry resource attributes that will be attached to logs as structured metadata.

After adding your application type, it will be automatically included in the generated dataset with the appropriate distribution based on the generator configuration.

### Adding New Storage Implementations

To add a new storage implementation:

1. Implement the `Store` interface in `store.go`
2. Add the new store to the `cmd/generate/main.go` file
3. Update the benchmark test to include the new store type

### Adding New Query Types

To add new query types:

1. Modify the `GenerateTestCases` method in `generator_query.go`
2. Add new query patterns that test different aspects of LogQL

## Troubleshooting

- If you see "Data directory is empty" errors, run `make generate` first
- For memory issues, try generating a smaller dataset with `make generate SIZE=524288000`
- For detailed logs, run benchmarks with `make run-debug`

## Performance Profiles

The benchmark suite can generate CPU and memory profiles:

- CPU profile: `cpu.prof`
- Memory profile: `mem.prof`

These can be analyzed with:

```bash
go tool pprof -http=:8080 cpu.prof
go tool pprof -http=:8080 mem.prof
```
