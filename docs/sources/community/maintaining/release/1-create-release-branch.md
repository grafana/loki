# Create Release Branch

A single release branch is created for every `major` or `minor` release(not for patched release). That release
branch is then used for all the Stable Release, and all Patch Releases for that `major` and `minor` versions of the Grafana Loki.

## Before you begin

1. Determine the [VERSION_PREFIX](concepts/version.md).
1. Announce about the upcoming release in `#loki-releases` slack channel

## Steps

1. Determine which commit should be used as a base for the release branch. Usually some `kxx` weekly release branch.

1. Create and push the release branch from the selected base commit:

    The name of the release branch should be `release-VERSION_PREFIX`, such as `release-2.9.x`.

	> **NOTE**: Branches are only made for VERSION_PREFIX; do not create branches for the full VERSION such as `release-v2.9.1` or `release-2.9.1`.

    > **NOTE**: Don't create any other branches that are prefixed with `release` when creating PRs or
    those branches will collide with our automated release build publish rules.

1. Create a label to make backporting PRs to this branch easy.

   The name of the label should be `backport release-VERSION_PREFIX`, such as `backport release-2.9.x`.

   > **NOTE**: Note there is space in the label name. It should be exactly same to trigger some CI related jobs.
