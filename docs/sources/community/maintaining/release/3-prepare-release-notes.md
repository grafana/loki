# Prepare Release notes

Release notes are the few key highlights of the release. This is what appears on the release page on the Github.

## Before you begin

1. Determine the [VERSION](concepts/version.md).

1. Know bit about how release notes works in Grafana Loki.

	We have
	1. `release-notes/next.md` to track release notes that are not released yet.
	1. `release-notes/v<major>-<minor>.md` to track release notes that is part of that Loki version.

	Preparing release notes for specific Loki release at high level is basically two steps
	1. Add important notes to `next.md`
	1. And make it available in specific Loki version.

## Steps

1. Discuss with Loki team to finalize what PRs should be part of release notes.

1. Go to the PR and add a label `add-to-release-notes`. Example [PR](https://github.com/grafana/loki/pull/10213) with label added.

	>>**NOTE**: Pick any one PR if the changes involves multiple PRs.

1. CI creates a PR to add original PR to release notes. Example [PR](https://github.com/grafana/loki/pull/10359)

1. Review the PR carefully. Update the description, approve and merge.

1. Repeat the steps for all the PRs.

1. Review the final release notes with Loki squad.

1. Prepare release notes PR to `main` branch. Example [PR](https://github.com/grafana/loki/pull/9004/)
   * Copy `next.md` to the new `v<MAJOR>-<MINOR>.md` (e.g: `v2.9.md`)
   * Replace version place holders `V?.?` with `V<MAJOR>.<MINOR>` in the new file.
   * Remove all the entries under `Features and enhancements`, `Upgrade Considerations` and `Bug fixes` on `next.md`
   * Add an entry for new version in `_index.md` as shown in the example PR above.
   * Get review and merged.

1. Backport above PR to the `release-VERSION_PREFIX` branch. Example [PR](https://github.com/grafana/loki/pull/10482)
