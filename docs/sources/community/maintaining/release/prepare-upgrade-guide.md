---
title: Prepare Upgrade guide
description: Prepare Upgrade guide
---
# Prepare Upgrade guide

The upgrade guide records changes that require user attention or interaction to upgrade to specific Loki version from previous versions.

## Before you begin

The upgrade guide in Grafana Loki works as follows.

We have a `setup/upgrade/_index.md` file that records upgrade information for all Loki releases.

## Steps

1. Make sure the upgrade guide is up to date on the `release-VERSION_PREFIX` branch under the `Main/Unreleased` section.

1. On the `release-VERSION_PREFIX` branch promote `Main/Unreleased` to `VERSION`. Example [PR](https://github.com/grafana/loki/pull/10470).

1. On the `main` branch remove entries from `Main/Unreleased` that are already part of `VERSION (YYY-MM-DD)`.
