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

## Release stable version

1. [Create release branch]({{< relref "./create-release-branch" >}})
1. [Backport commits]({{< relref "./backport-commits" >}})
1. [Prepare Release notes]({{< relref "./prepare-release-notes" >}})
1. [Prepare Changelog]({{< relref "./prepare-changelog" >}})
1. [Document Metrics and Configurations changes]({{< relref "./document-metrics-configurations-changes" >}})
1. [Prepare Upgrade guide]({{< relref "./prepare-upgrade-guide" >}})
1. [Update version numbers]({{< relref "./update-version-numbers" >}})
1. [Tag Release]({{< relref "./tag-release" >}})
1. [Publish Release]({{< relref "./publish-release" >}})

## Release patched version

1. [Backport commits]({{< relref "./backport-commits" >}})
1. [Prepare Release notes]({{< relref "./prepare-release-notes" >}})
1. [Prepare Changelog]({{< relref "./prepare-changelog" >}})
1. [Document Metrics and Configurations changes]({{< relref "./document-metrics-configurations-changes" >}})
1. [Prepare Upgrade guide]({{< relref "./prepare-upgrade-guide" >}})
1. [Update version numbers]({{< relref "./update-version-numbers" >}})
1. [Tag Release]({{< relref "./tag-release" >}})
1. [Publish Release]({{< relref "./publish-release" >}})

## Release security patched version

1. [Patch vulnerabilities]({{< relref "./patch-vulnerabilities" >}})
1. [Backport commits]({{< relref "./backport-commits" >}})
1. [Prepare release notes]({{< relref "./prepare-release-notes" >}})
1. [Prepare changelog]({{< relref "./prepare-changelog" >}})
1. [Update version numbers]({{< relref "./update-version-numbers" >}})
1. [Tag release]({{< relref "./tag-release" >}})
1. [Publish release]({{< relref "./publish-release" >}})
