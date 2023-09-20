---
title: Check metrics and configurations changes
description: Check metrics and configurations changes
---
# Check metrics and configurations changes

Any metrics and configurations that are removed or modified need to be part of upgrade guide. Configurations whose default values changed should also be part of upgrade guide.

## Before you begin

All the steps are performed on `release-VERSION_PREFIX` branch.

## Steps

1. Check configs changed, including whose default values changed.
   ```
   $ OLD_VERSION=X.Y.Z ./tools/diff-config.sh
   ```

1. Record configurations that are modified(either renamed or it's default value changed) in the [upgrade guide]({{< relref "./6-prepare-upgrade-guide" >}})

1. Check metrics changed
   ```
   $ OLD_VERSION=X.Y.Z ./tools/diff-metrics.sh
   ```

1. Record metrics whose names modified in the [upgrade guide]({{< relref "./6-prepare-upgrade-guide" >}})
