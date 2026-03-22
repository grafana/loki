# Bigtable client contribution guide

For a complete guide to contributing to Google Cloud Go client libraries, view
the [top-level contributing guide](../CONTRIBUTING.md).

## Running Integration Tests
The Bigtable integration tests will target the emulator by default. Some of the
tests can only be run against production however. In order to do this you will
need to specify a few command line flags.

```
go test -test.run="TestIntegration_*" -v \
    -it.use-prod \
    -it.project="your-project-id" \
    -it.cluster="your-test-cluster" \
    -it.instance="your-test-instance"
```

> **Note**: More flags exist and can be found in `export_test.go`

If you do not have a cluster and instance to target, you can create one via `cbt`:

```
# Creates a one node cluster in us-central1 with SSD storage
cbt createinstance <instance-name> <display-name> <cluster-name> us-central1-b 1 SSD
```