---
title: Prepare Changelog
description: Prepare Changelog
---
# Prepare Changelog

Changelog is the list of all the important changes (features, bug-fix, optimizations, docs) that are part of particular Loki release.

## Before you begin

1. Determine the [VERSION_PREFIX]({{< relref "./concepts/version" >}}).

1. The changelog in Grafana Loki works as follows.

	We have `CHANGELOG.md` that records both unreleased and released changes.

	Preparing changelog for Loki release at high level is basically two steps
	1. Move `unreleased` changes to specific version on `release-VERSION_PREFIX` branch.
	1. Reflect those changes on `main` branch.

## Steps

1. Make sure the `CHANGELOG` on `release-VERSION_PREFIX` branch is up to date.

	1. Check the commits diffs between the new version (example: 2.9.x) and the most recent older version (example: 2.8.x) via
	```
	https://github.com/grafana/loki/compare/release-2.8.x...release-2.9.x
	```
	1. Check if any of those commits are important enough to add to the `CHANGELOG`.

1. On the `release-VERSION_PREFIX` branch promote `Main/Unreleased` to `VERSION (YYY-MM-DD)`. Example [PR](https://github.com/grafana/loki/pull/10470).

1. On the `main` branch remove entries from `Main/Unreleased` that are already part of `VERSION (YYY-MM-DD)`. Example [PR](https://github.com/grafana/loki/pull/10497).
