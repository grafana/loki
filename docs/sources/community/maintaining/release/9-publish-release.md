---
title: Publish Release
description: Publish Release
---
# Publish Release

This is how to publish the release in GitHub.

## Before you begin

1. You should see a new draft release created [here](https://github.com/grafana/loki/releases). If not, go back to [Tag Release]({{< relref "./8-tag-release" >}}).

## Steps

1. Edit the release draft by filling in the `Notable Changes` section with all the changes from the release notes for this release (from the `docs/sources/release-notes/` directory).

1. Add a footer to the `Notable Changes` section:

    `For a full list of changes, please refer to the [CHANGELOG](https://github.com/grafana/loki/blob/RELEASE_VERSION/CHANGELOG.md)!`

    Do not substitute the value for `CHANGELOG`.

1. At the bottom of the release page, perform the following:
    - For a Stable Release or Patch Release, tick the checkbox to "set as the latest release" only if it's the latest release on current latest `release-VERSION_PREFIX` branch.

2. Optionally, have other team members review the release draft if you wish to feel more comfortable with it.

3. Publish the release!
