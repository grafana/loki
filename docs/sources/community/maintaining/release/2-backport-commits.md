---
title: Backport commits
description: Backport commits
---
# Backport commits

Any PRs or commits not on the release branch that you want to include in the release must be backported to the release branch.

## Before you begin

1. Determine the [VERSION_PREFIX]({{< relref "./concepts/version" >}}).

2. If the release branch already has all the code changes on it, skip this step.

## Steps

1. Pick a PR that you want to backport to `release-VERSION_PREFIX` branch.

1. Add a label `backport release-VERSION_PREFIX` to that PR. You have to add one of the additional labels `product-approved`, `type/doc` or `type/bug` appropriately. This is to make sure the PRs that are backported are done with right intention.
   Now CI should automatically create backport PR to the correct release branch. Example [PR](https://github.com/grafana/loki/pull/10333)

   > **NOTE**: CI automation can fail sometimes if there are some merge conflicts in cherry picking the commits. In those cases, the original PR where you added the label should have additional comment explaining how to backport it manually.

   > **NOTE**: The CI job that helps with backporting PR is `.github/workflows/backport.yml`. Useful for debugging purposes.

1. Repeat the above steps for any PRs that need to be backported.
