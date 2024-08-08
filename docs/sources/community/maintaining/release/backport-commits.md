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

1. Add two labels to the PR. First, one of the `product-approved`, `type/doc` or `type/bug` appropriately. This is to make sure the PRs that are backported are done with right intention. Second `backport release-VERSION_PREFIX` label.
   Now CI should automatically create backport PR to the correct release branch. Example [PR](https://github.com/grafana/loki/pull/10333)

	{{% admonition type="note" %}}
	CI automation can fail sometimes if there are some merge conflicts in cherry picking the commits. In those cases, the original PR where you added the label should have additional comment explaining how to backport it manually.
	{{% /admonition %}}

	{{% admonition type="note" %}}
	The CI job that helps with backporting PR is `.github/workflows/backport.yml`. Useful for debugging purposes.
   	{{% /admonition %}}

1. Repeat the above steps for any PRs that need to be backported.


## Backporting Release PRs

If backporting a release PR, make sure you remove any `autorelease: pending` or `autorelease: tagged` labels before merging the backport PR. By default our backport action brings over all labels, but these labels are reserved for the release workflow and will cause future pipelines to fail if left of backport PRs.
