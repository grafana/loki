# Testing

Package storage has unit, emulated integration tests, and integration tests
against the real GCS service.

## Setup

Assume that you're running from a directory which contains the `google-cloud-go`
git repository.

```bash
git clone https://github.com/googleapis/google-cloud-go
git clone https://github.com/googleapis/storage-testbench  # emulator
```

## Running unit tests

```bash
go test ./google-cloud-go/storage -short
```

## Running emulated integration tests

See
https://github.com/googleapis/storage-testbench?tab=readme-ov-file#how-to-use-this-testbench
for testbench setup instructions. After following those instructions, you should
have an emulator running an HTTP server on port 9000 and a gRPC server on port
8888.

```bash
STORAGE_EMULATOR_HOST_GRPC="localhost:8888" STORAGE_EMULATOR_HOST="http://localhost:9000" go test ./google-cloud-go/storage -short -run="^Test(RetryConformance|.*Emulated)"
```

If you don't specify the `-run` filter, this will also run unit tests.

## Running live service integration tests

See the [general setup instructions](../CONTRIBUTING.md#local-setup) for more
details. The GCS integration tests require:

- A project configured such that all bucket types can be created (e.g. with and
  without UBLA, with and without HNS). A dedicated project which only stores
  test data is recommended.
- A JSON key file for a service account with most GCS privileges in that
  project.
- A VM in that project.

Run with:

```bash
GCLOUD_TESTS_GOLANG_PROJECT_ID="${PROJECT_ID?}" GCLOUD_TESTS_GOLANG_KEY="${KEYFILE?}" \
  go test ./google-cloud-go/storage -run="^Test.*Integration"
```
