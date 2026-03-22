---
title: Prepare Release
description: Describes the prepare release pipeline that prepares release PRs for Grafana Loki.
---
# Prepare Release

Releasing Grafana Loki consists of merging a long-running release PR. Two workflows keep these PRs up to date, one for patch releases (which runs on commits to branches matching `release-[0-9]+.[0-9]+.x`) and one for minor releases (which runs on commits to branches matching `k[0-9]+`). These pipelines use release please to do the following:

1. Run tests and linting
1. Build binaries and images for the proposed release version
1. Generate release notes based on conventional commits since the last release
1. Create or update the long-running release PR for that release, indicating which commit will be released if the PR is merged with a link to the built artifacts that will be published.

## Major releases

Major releases follow the same process as minor and patch releases, but require a custom workflow to be created to run on the branch we want to release from. The reason for this is that we don't do major releases very often, so it is not economical to keep those workflows running all the time.To create a major release workflow, follow the steps in the [major release workflow](../major-release/) document.
