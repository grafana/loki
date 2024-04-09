# Deprecated Config Checker

This script can check your configuration files for deprecated and deleted options.

## Usage

Run the script with `-help` for a list of options.

### Example

```bash
go run tools/deprecated-config-checker/main.go \
  -config.file tools/deprecated-config-checker/test-fixtures/config.yaml \
  -runtime-config.file tools/deprecated-config-checker/test-fixtures/runtime-config.yaml
```

## Adding a new config deprecation or deletion?

Add deprecations to the `deprecated-config.yaml` file, and deletions to the `deleted-config.yaml`. 
Then, update the `test-fixtures/config.yaml` and `test-fixtures/runtime-config.yaml` files as well as
the tests in `checker/checker_test.go`.
