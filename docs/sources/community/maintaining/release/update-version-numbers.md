---
title: Update version numbers
description: Update version numbers
---
# Update version numbers

Upgrade the Loki version to the new release version in documents, examples, jsonnets, etc.

## Before you begin

1. Determine the [VERSION_PREFIX]({{< relref "./concepts/version" >}}).

2. Skip this step if you are doing a patch release on old release branch.

	Example: Latest release branch is `release-2.9.x` but you are releasing patch version in `release-2.8.x` (or older)

## Steps

1. Upgrade the versions in the `release-VERSION_PREFIX` branch.

    Example commands:

    ```
	LOKI_NEW_VERSION=$VERSION ./tools/release_update_tags.sh
    ```

	{{< admonition type="note" >}}
	Do not upgrade the version numbers in the `operator/` directory as @periklis and team have a different process to upgrade the Operator version.
	{{< /admonition >}}
