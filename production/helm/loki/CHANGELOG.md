# Changelog

All notable changes to this library will be documented in this file.

Entries should be ordered as follows:

- [CHANGE]
- [FEATURE]
- [ENHANCEMENT]
- [BUGFIX]

Entries should include a reference to the pull request that introduced the change.

## 1.7.5

- [ENHANCEMENT] Update dashboards, include recording and alerting rules

## 1.7.4

- [CHANGE] Default accessKeyId, endpoint, and secretAccessKey s3 setting to null to make them easier to provide via env vars

## 1.7.3

- [BUGFIX] Fix a bug in how object storage client was defined for the ruler

## 1.7.2

- [ENHANCEMENT] Memcache can now easily be configured as an external caching option.
=======
## 1.6.3

- [CHANGE] The `prometheusRule` value had been removed. If you were using this to add custom recording rules, move `prometheusRule.groups` to `monitoring.rules.additionalGroups`, which will merge your custom groups with the ones that now ship out of the box with this chart.
>>>>>>> 41f3c7fc (Include required CRDs, condense rules to 1 file)

## 1.6.0

- [FEATURE] Added self-monitoring option, with dashboards and Grafana Agent Operator custom resources to allow Loki to scrape it's own logs.
- [CHANGE] Move `serviceMonitor` configuraiton under `monitoring` section, which now also includes a section for `dashboards` and `selfMonitoring`.

## 1.5.0

-[CHANGE] Set `persistentVolumeClaimRetentionPolicy` for read pods to `Delete` for both `whenDeleted` and `whenScaled` since this data is easy to replace by re-fetching from object storage. This should make for easier roll-outs of the stateful sets.

## 1.3.0

- [BUGFIX] Fix the storage heper functions to use the right properties (snake vs camel case) #1420
- [ENHANCEMENT] Add this changelog to the chart #1420
