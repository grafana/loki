---
title: Patch Go version
description: Describes the procedure how to patch the Go version in Loki.
---
# Patch Go version

Update vulnerable Go version to non-vulnerable Go version to build Grafana Loki binaries.

## Steps

1. Find Go version to which you need to update. Example `1.20.5` to `1.20.6`
1. Update Go version (`GO_VERSION`) in the `Makefile`.
1. Run `make release-workflows` to update the version in generated release workflows.
1. Run `make update-go-version` to update all relevant files.
1. Open a pull request against the target branch (`main` or `release-MAJOR.MINOR.x`).
