---
title: Prepare Upgrade guide
description: Prepare Upgrade guide
---
# Prepare Upgrade guide

Upgrade guide records changes that require user attention or interaction to upgrade to specific Loki version from previous versions.

## Before you begin

Know bit about how upgrade works in Grafana Loki.

We have `setup/upgrade/_index.md` that records upgrade guide for every Loki releases.

## Steps

1. Make sure upgrade guide is up to date on `release-VERSION_PREFIX` branch under `Main/Unreleased` section.

1. On the `release-VERSION_PREFIX` branch promote `Main/Unreleased` to `VERSION`. Example [PR](https://github.com/grafana/loki/pull/10470)

1. On the `main` branch remove entries from `Main/Unreleased` that are already part of `VERSION (YYY-MM-DD)`.
