---
title: Tag Release
description: Tag Release
---
# Tag Release

A tag is required to create GitHub artifacts and as a prerequisite for publishing.

## Before you begin

1. All required commits for the release should exist on the release branch. This includes functionality and documentation such as the [release notes]({{< relref "./3-prepare-release-notes" >}}) and [CHANGELOG.md]({{< relref "./4-prepare-changelog" >}}). All versions in the repo should have already been [updated]({{< relref "./7-prepare-version-upgrades" >}}).

1. Make sure you are up to date on the release branch:

   ```
   git checkout release-VERSION_PREFIX
   git fetch origin
   git pull origin
   ```

1. Determine the [VERSION_PREFIX]({{< relref "./concepts/version" >}}).

1. Follow the GitHub [instructions](https://docs.github.com/en/authentication/managing-commit-signature-verification) to set up GPG for signature verification.

1. Optional: Configure git to always sign on commit or tag.

```bash
git config --global commit.gpgSign true
git config --global tag.gpgSign true
```

If you are on macOS or linux and using an encrypted GPG key, `gpg-agent` or `gpg` may be unable
to prompt you for your private key passphrase. This will be denoted by an error
when creating a commit or tag. To circumvent the error, add the following into
your `~/.bash_profile`, `~/.bashrc` or `~/.zshrc`, depending on which shell you are using.

```
export GPG_TTY=$(tty)
```

## Steps

1. Tag the release:

    Example commands:

    ```
	RELEASE="v$VERSION" # e.g: v2.9.0
    git tag -s $RELEASE -m "tagging release $RELEASE"
    git push origin $RELEASE
    ```

1. After a tag has been pushed, GitHub CI will create release assets and open a release draft for every pushed tag.

    - This will take ~10-20 minutes.
    - You can monitor this by viewing the drone build on the commit for the release tag.
	- It also creates PR to update helm charts. Example [PR](https://github.com/grafana/loki/pull/10479). Review and merge it.
