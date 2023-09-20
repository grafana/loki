# Prepare version upgrades

Upgrade the Loki version to this release version on documents, examples, jsonnets, etc.

## Before you begin

1. Determine the [VERSION](concepts/version.md).

2. Skip this step if you are doing patch release on old release branch.

	Example: Latest release branch is `release-2.9.x` but you are releasing patch version on `release-2.8.x` (or older)

## Steps

1. Upgrade the versions on `release-VERSION_PREFIX` branch

    Example commands:

    ```
	LOKI_NEW_VERSION=$VERSION ./tools/release_update_tags.sh
    ```

	>**NOTE** We leave out any upgrades on `operator/` directory as @periklis and team have different process to upgrade versions.
