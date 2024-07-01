---
title: Merge Release PR
description: Describes the process to release Loki by merging the release PR.
---
# Merge Release PR

To release Loki, merge the release PR. This PR will have the title `chore(<BRANCH>): release <VERSION>`. Here's what the 3.0 release PR looked like.

![3.0 release PR](.3.0-release-pr.png)

3.0 was the first major release using this new process, and enforcing conventional commits was relatively new. Going forward the release notes in the release PR will be much more thorough, as we're now enforcing that every PR has a conventional commit message.

To test the artifacts before releasing, you can download the artifacts from the link in the footer of the PR description. Merging the PR will:

1. Fetch the built artifacts.
1. Create a draft GitHub release with the release notes in the PR description.
1. Upload fetched binareis to the draft release.
1. Publish fetched images to Docker Hub as multi-arch images.
1. Publish the draft release and create the GitHub tag.
1. (Optionally) Mark the release as the latest if it represents the newest version of Loki.

## Troubleshooting / Retrying Release

If something goes wrong with the release automation, you may need to rerun the job. This may require updating the release code in the `grafana/loki` repo via a PR, or it may include updating the code in `grafana/loki-release` that the release pipeline fetches. In either case, if you need to re-release a merge release PR, you'll need to remove the `autorelease: tagged` label from that PR and add the `autorelease: pending` label. The automation relies on these labels to determine which merged PRs have and have not yet been released. After fixing the labels, re-run the release workflow, and it should correctly get passed the `should-release` step. The release process is idempotent, so if a draft release has already been created via a previously failed release, the process will continue, re-upload the binaries, and re-publish the images.

Once the release is finished, you may need to manually flip the label back to `autorelease: tagged`.
