---
title: Version
description: Version
---
# Version

Grafana Loki uses Semantic Versioning. The next version can be determined
by looking at the current version and incrementing it. When the release is a new `major` version (for example, after 2.9.1 we decide to go 3.0.0 instead of 2.9.2) this should not be done.

You need to set two environmental values `VERSION` and `VERSION_PREFIX` to do the release process.

## Version

To determine the `VERSION` for a Stable Release or Patch Release, use the Semantic Version `A.B.C`.

- Examples
  - For example, `2.9.0` is the Stable Release `VERSION` for the v2.9.0 release.
  - For example, `2.9.1` is the first Patch Release `VERSION` for the v2.9.0 release.

## Version Prefix

To determine the `VERSION PREFIX`, use only the major and minor version `A.B.x`.

- Examples
  - `2.9.x`
