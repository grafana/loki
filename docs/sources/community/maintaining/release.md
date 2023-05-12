---
title: Releasing Grafana Loki
description: Releasing Grafana Loki
aliases: 
- /docs/loki/latest/maintaining
- /docs/loki/latest/community/maintaining
---
# Releasing Grafana Loki

This document is a series of instructions for core Grafana Loki maintainers to be able
to publish a new Loki release.

## Prerequisites

Each maintainer performing a release should perform the following steps once
before releasing Loki.

### Signing Tags and Commits

#### Add Existing GPG Key to GitHub

First, Navigate to your user's [SSH and GPG keys settings
page](https://github.com/settings/keys). If the GPG key for the email address
used to commit with Loki is not present, follow these instructions to add it:

1. Run `gpg --armor --export <your email address>`
1. Copy the output.
1. In the settings page linked above, click "New GPG Key".
1. Copy and paste the PGP public key block.

#### Signing Commits and Tags by Default

To avoid accidentally publishing a tag or commit without signing it, you can run
the following to ensure all commits and tags are signed:

```bash
git config --global commit.gpgSign true
git config --global tag.gpgSign true
```

##### macOS Signing Errors

If you are on macOS and using an encrypted GPG key, the `gpg-agent` may be
unable to prompt you for your private key passphrase. This will be denoted by an
error when creating a commit or tag. To circumvent the error, add the following
into your `~/.bash_profile` or `~/.zshrc`, depending on which shell you are
using:

```
export GPG_TTY=$(tty)
```

## Performing the Release

1. Create a new branch to update `CHANGELOG.md` and references to version
   numbers across the entire repository (e.g. README.md in the project root).
1. Modify `CHANGELOG.md` with the new version number and its release date.
1. List all the merged PRs since the previous release. This command is helpful
   for generating the list (modifying the date to the date of the previous release): `curl https://api.github.com/search/issues?q=repo:grafana/loki+is:pr+"merged:>=2019-08-02" | jq -r ' .items[] | "* [" + (.number|tostring) + "](" + .html_url + ") **" + .user.login + "**: " + .title'`
1. Go through `docs/` and find references to the previous release version and
   update them to reference the new version.
1. *Without creating a tag*, create a commit based on your changes and open a PR
   for updating the release notes.
   1. Until [852](https://github.com/grafana/loki/issues/852) is fixed, updating
      Helm and Ksonnet configs needs to be done in a separate commit following
      the release tag so that Helm tests pass.
1. Merge the changelog PR.
1. Create a new tag for the release.
    1. Once this step is done, the CI will be triggered to create release
       artifacts and publish them to a draft release. The tag will be made
       publicly available immediately.
    1. Run the following to create the tag:

       ```bash
       RELEASE=v1.2.3 # UPDATE ME to reference new release
       git checkout release-1.2.x # checkout release branch
       git pull
       git tag -s $RELEASE -m "tagging release $RELEASE"
       git push origin $RELEASE
       ```
1. Watch Drone and wait for all the jobs to finish running.

## Updating Helm and Ksonnet configs

These steps should be executed after the previous section, once CircleCI has
finished running all the release jobs.

1. Run `bash ./tools/release_prepare.sh`
1. When prompted for the release version, enter the latest tag.
1. When prompted for new Helm version numbers, the defaults should suffice (a
   minor version bump).
1. Commit the changes to a new branch, push, make a PR, and get it merged.

## Publishing the Release Draft

Once the previous two steps are completed, you can publish your draft!

1. Go to the [GitHub releases page](https://github.com/grafana/loki/releases)
   and find the drafted release.
1. Edit the drafted release, copying and pasting *notable changes* from the
   CHANGELOG. Add a link to the CHANGELOG, noting that the full list of changes
   can be found there. Refer to other releases for help with formatting this.
1. Optionally, have other team members review the release draft so you feel
   comfortable with it.
1. Publish the release!

## Versioning Loki docs on Grafana Website

Loki docs are versioned. Follow the below steps to version Loki docs for this release.

>NOTE: Here $LOCAL_LOKI_PATH is your local path where Loki is checked out with correct $VERSION

1. Clone Grafana website [repo](https://github.com/grafana/website)
1. Create new branch `git checkout -b $VERSION` (replace `$VERSION` with current release version. e.g: `v2.5.0`)
1. Run `mv content/docs/loki/next content/docs/loki/next.main`
1. Run `mkdir content/docs/loki/next`
1. Run `cp -R $LOCAL_LOKI_PATH/docs/sources/* content/docs/loki/next`
1. Run `scripts/docs-release.sh loki latest next`
1. Run `scripts/docs-release.sh loki $VERSION latest`
1. Run `mv content/docs/loki/next.main content/docs/loki/next`
1. Update `version_latest` to `$VERSION` in `content/docs/loki/_index.md`
1. Docs will be generated for this release.
1. Create PR and Merge it after approval.
