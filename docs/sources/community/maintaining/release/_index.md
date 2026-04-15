---
title: Releasing Grafana Loki
description: Releasing Grafana Loki
aliases:
- ../../maintaining/release/
weight: 500
---
# Releasing Grafana Loki

This document is a series of instructions for core Grafana Loki maintainers to be able
to publish a new [Grafana Loki](https://github.com/grafana/loki) release.

The general process for releasing a new version of Grafana Loki is to merge the release PR for that version. Every commit to branches matching the pattern `release-[0-9]+.[0-9]+.x` will trigger a [prepare patch release](prepare-release/) workflow. This workflow will build release candidates for that patch, automatically generate release notes based on the commits since the last release, and update the long-running PR for that release. To publish the release, merge the PR.

Every commit to branches matching the pattern `k[0-9]+` will trigger a [prepare minor release](prepare-release/) workflow. This follows the same process as a patch release, but prepares a minor release instead. To publish the minor release, merge the PR.

Releasing a new major version requires a [custom major release workflow](major-release/) to be created to run of the branch we want to release from. Once that workflow is created, the steps for releasing a new major are the same as a minor or patch release.

## Release stable version

1. [Create release branch](create-release-branch/)
1. [Backport commits](backport-commits/)
1. [Document Metrics and Configurations changes](document-metrics-configurations-changes/)
1. [Prepare Upgrade guide](prepare-upgrade-guide/)
1. [Update version numbers](update-version-numbers/)

## Release patched version

1. [Backport commits](backport-commits/)
1. [Document Metrics and Configurations changes](document-metrics-configurations-changes/)
1. [Prepare Upgrade guide](prepare-upgrade-guide/)
1. [Merge Release PR](merge-release-pr/)
1. [Update version numbers](update-version-numbers/)

## Release security patched version

1. [Patch vulnerabilities](patch-vulnerabilities/)
1. [Backport commits](backport-commits/)
1. [Merge Release PR](merge-release-pr/)
1. [Update version numbers](update-version-numbers/)
