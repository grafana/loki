# Tests

Tests that run on Travis have been split into jobs that run in their own VM in parallel. This reduces the overall running time but also is allowing recycling of a job when we get a flapper as opposed to have to recycle the whole test suite.

## JetStream Tests

For JetStream tests, we need to observe a naming convention so that no tests are omitted when running on Travis.

The script `runTestsOnTravis.sh` will run a given job based on the definition found in "`.travis.yml`".

As for the naming convention:

- All JetStream tests name should start with `TestJetStream`
- Cluster tests should go into `jetstream_cluster_test.go` and start with `TestJetStreamCluster`
- Super-cluster tests should go into `jetstream_super_cluster_test.go` and start with `TestJetStreamSuperCluster`

Not following this convention means that some tests may not be executed on Travis.
