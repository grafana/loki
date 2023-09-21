---
title: Prepare Release notes
description: Prepare Release notes
---
# Prepare Release notes

Release notes are the few key highlights of the release. This is what appears on the release page on Github.

## Before you begin

1. Determine the [VERSION_PREFIX]({{< relref "./concepts/version" >}}).

1. The release notes process for Grafana Loki works as follows.

	We have two separate markdown files:
	1. `release-notes/next.md` to track release notes that are not released yet.
	1. `release-notes/v<major>-<minor>.md` to track release notes that are part of each specific Loki version.

	Preparing release notes for a specific Loki release at high level is basically two steps:
	1. Add important notes to `next.md`
	1. And make it available in a specific Loki version.

## Steps

1. Discuss with Loki team to finalize what PRs should be part of release notes.

1. Go to the PR and add a label `add-to-release-notes`. Example [PR](https://github.com/grafana/loki/pull/10213) with label added.

	{{% admonition type="note" %}}
	Pick any one PR if the changes involves multiple PRs.
	{{% /admonition %}}

1. The CI process creates a PR to add the original PR to the release notes. Example [PR](https://github.com/grafana/loki/pull/10359).

1. Review the PR carefully. Update the description, approve and merge.

1. Repeat the steps for all the PRs.

1. Review the final release notes with Loki squad.

1. Create a release notes PR on the `main` branch. Example [PR](https://github.com/grafana/loki/pull/9004/).
   * Copy `next.md` file to the new file `v<MAJOR>-<MINOR>.md` (e.g: `v2.9.md`)
   * Replace version place holders `V?.?` with `V<MAJOR>.<MINOR>` in the new file.
   * Remove all the entries under `Features and enhancements`, `Upgrade Considerations` and `Bug fixes` in `next.md` file
   * Add an entry for the new version in `_index.md` as shown in the example PR above.  Set the weight for the new file so that the new release notes file appears at the top of the table of contents.
   * Get the PR reviewed and merged.

1. Backport the release notes PR from `main` to the `release-VERSION_PREFIX` branch. Example [PR](https://github.com/grafana/loki/pull/10482).
