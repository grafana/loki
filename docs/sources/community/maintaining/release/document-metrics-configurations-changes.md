---
title: Document metrics and configurations changes
description: Document metrics and configurations changes
---
# Document metrics and configurations changes

Any metrics and configurations that are removed or modified need to be documented in the upgrade guide. Configurations whose default values changed should also be documented in the upgrade guide.

## Before you begin

All the steps are performed on `release-VERSION_PREFIX` branch.

## Steps

1. Check which configs changed, including whose default values changed.
   ```
   $ OLD_VERSION=X.Y.Z ./tools/diff-config.sh
   ```

1. Record configurations that are modified (either renamed or had its default value changed) in the [upgrade guide](../prepare-upgrade-guide/).

1. Check if any metrics have changed.
   ```
   $ OLD_VERSION=X.Y.Z ./tools/diff-metrics.sh
   ```

1. Record metrics whose names have been modified in the [upgrade guide](../prepare-upgrade-guide/).
