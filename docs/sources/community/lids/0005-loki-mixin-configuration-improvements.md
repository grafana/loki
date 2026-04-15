---
title: "0005: Loki mixin configuration improvements"
description: "Improve Loki mixin configurations"
draft: false
---

# 0005: Loki mixin configuration improvements

**Author:** Alexandre Chouinard (Daazku@gmail.com)

**Date:** 03/2025

**Sponsor(s):** N/A

**Type:** Feature

**Status:** Draft

**Related issues/PRs:**

- [Issue #15881](https://github.com/grafana/loki/issues/15881)
- [Issue #13631](https://github.com/grafana/loki/issues/13631)
- [Issue #11820](https://github.com/grafana/loki/issues/11820)
- [Issue #11806](https://github.com/grafana/loki/issues/11806)
- [Issue #7730](https://github.com/grafana/loki/issues/7730)
- and more ...

**Thread from [mailing list](https://groups.google.com/forum/#!forum/lokiproject):** N/A

---

## Background

There is no easy way to set up dashboards and alerts for Loki on a pre-existing Prometheus stack that does not use the Prometheus Operator with a specific configuration.

The metrics selectors are hardcoded, making the dashboard unusable without manual modifications in many cases.
It is assumed that `job`, `cluster`, `namespace`, `container` and/or a combination of other labels are present on metrics and have very specific values.

## Problem Statement

This renders the dashboards and alerts unusable for setups that do not conform to the current assumptions about which label(s) should be present in the metrics.

A good example of that would be the "job" label used everywhere:
[`job=~\"$namespace/bloom-planner\"`](https://github.com/grafana/loki/blob/475d25f459575312adb25ff90abf8f10d521ad4b/production/loki-mixin/dashboards/dashboard-bloom-build.json#L267C101-L267C134)

Usually the job label refer to the task name used to scrape the targets, as per [Prometheus documentation](https://prometheus.io/docs/concepts/jobs_instances/), and
in k8s, if you are not using `prometheus-operator` with `ServiceMonitor`, it's pretty common to have something like this as a scraping config:

```yaml
        - job_name: "kubernetes-pods" # Can actually be anything you want.
          kubernetes_sd_configs:
            - role: pod
          relabel_configs:
            # Cluster label is "required" by kubernetes-mixin dashboards
            - target_label: cluster
              replacement: '${cluster_label}'
            ...
```

which would scrape all pods and yield something like:

```
up{job="kubernetes-pods", ...}
```

Right off the bat, that makes the dashboards unusable because it's incompatible with what is **hardcoded** in the dashboards and alerts.

## Goals

Ideally, selectors should default to the values required internally by Grafana but remain configurable so users can tailor them to their setup.

A good example of this is how [kubernetes-monitoring/kubernetes-mixin](https://github.com/kubernetes-monitoring/kubernetes-mixin/blob/1fa3b6731c93eac6d5b8c3c3b087afab2baabb42/config.libsonnet#L20-L33) did it:
Every possible selector is configurable and thus allow for various setup to properly work.

The structure is already there to support this. It just has not been leveraged properly.

## Non-Goals (optional)

It would be desirable to create some automated checks verifying that all metrics used in dashboard and alerts are using the proper selector(s) from the configuration.
There are many issues in the repository about new dashboards or dashboard updates not using the proper labels on metrics.

## Proposals

### Proposal 0: Do nothing

This forces the community to either manually edit the dashboards/alerts or conform to a specific metric collection approach for Loki.

### Proposal 1: Allow metrics label selectors to be configurable

This will require a good amount of refactoring.

It allows easier adoption of the "official" dashboards and alerts by the community.

Define once, reuse everywhere. (Currently, updating requires extensive search and replace.)

## Other Notes

If this proposal is accepted, I am willing to do the necessary work to move it forward.
