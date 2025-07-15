---
title: Patch vulnerabilities
description: Describes the procedure how to patch Loki to mitigate vulnerabilities.
---
# Patch vulnerabilities

This step patches vulnerabilities in Grafana Loki binaries and Docker images.

## Before you begin

1. Determine the [VERSION_PREFIX](../concepts/version/).

Vulnerabilities can be from two main sources.

1. Grafana Loki source code.

1. Grafana Loki dependencies.

Grafana Loki dependencies can be

1. Go dependencies in `go.mod`

1. The Go compiler itself

1. Grafana Loki Docker dependencies, for example, the base images

Before start patching vulnerabilities, know what are you patching. It can be one or more from sources mentioned above. Use `#security-go`, `#security` slack channels to clarify.

## Steps

1. Patch Grafana Loki source code.

	Means, there are vulnerabilities in Grafana Loki source code itself.

	1. Patch it on `main` branch

	1. [Backport](../backport-commits/) to `release-$VERSION_PREFIX` branch.

1. Patch Go dependencies.

	1. Pick all the Go dependencies that need to be patched.

	1. Check if [dependabot already patched the dependency](https://github.com/grafana/loki/pulls?q=is%3Apr+label%3Adependencies+is%3Aclosed) or [have a PR opened to patch](https://github.com/grafana/loki/pulls?q=is%3Apr+is%3Aopen+label%3Adependencies) . If not, manually upgrade the package on the `main` branch as follows.

		```shell
		go get -u -v <package-path>@<patched-version>
		go mod tidy
		go mod vendor
		```
	1. [Backport](../backport-commits/) it to `release-$VERSION_PREFIX` branch.

	1. Repeat for each Go dependency

1. [Patch Go compiler](../patch-go-version/).

1. Patch Grafana Loki Docker dependencies, for example: Alphine Linux base images).

   1. Update Docker image version. [Example PR](https://github.com/grafana/loki/pull/10573).

   1. [Backport](../backport-commits/) to `release-$VERSION_PREFIX` branch
