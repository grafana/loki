---
title: Releasing Grafana Loki
description: Releasing Grafana Loki
aliases:
- ../../maintaining/release/
---
# Releasing Grafana Loki

This document is a series of instructions for core Grafana Loki maintainers to be able
to publish a new [Grafana Loki](https://github.com/grafana/loki) release.

## Release stable version

1. [Create release branch](./1-create-release-branch.md)
1. [Backport PR(s)](./2-backport-prs.md)
1. [Prepare Release notes](./3-prepare-release-notes.md)
1. [Prepare Changelog](./4-prepare-changelog.md)
1. [Check Metrics and Configurations changes](./5-check-metrics-configurations-changes.md)
1. [Prepare Upgrade guide](./6-prepare-upgrade-guide.md)
1. [Prepare version upgrades](./7-prepare-version-upgrades.md)
1. [Tag Release](./8-tag-release.md)
1. [Publish Release](./9-publish-release.md)

## Release patched version

1. [Backport PR(s)](./2-backport-prs.md)
1. [Prepare Release notes](./3-prepare-release-notes.md)
1. [Prepare Changelog](./4-prepare-changelog.md)
1. [Check Metrics and Configurations changes]({{< relref "./5-check-metrics-configurations-changes" >}})
1. [Prepare Upgrade guide]({{< relref "./6-prepare-upgrade-guide" >}})
1. [Prepare version upgrades](./7-prepare-version-upgrades.md)
1. [Tag Release](./8-tag-release.md)
1. [Publish Release](./9-publish-release.md)
