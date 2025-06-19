---
title: Release cadence
description: How our release process works
weight: 1
---

# Release cadence

## Stable Releases

Loki releases (this includes [Promtail](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/promtail/), [Loki Canary](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/loki-canary/), etc.) use the following
naming scheme: `MAJOR`.`MINOR`.`PATCH`.

- `MAJOR` (roughly once a year): these releases include large new features and possible backwards-compatibility breaks.
- `MINOR` (roughly once a quarter): these releases include new features which generally do not break backwards-compatibility, but from time to time we might introduce _minor_ breaking changes, and we will specify these in our upgrade docs.
- `PATCH` (roughly once or twice a month): these releases include bug and security fixes which do not break backwards-compatibility.

{{< admonition type="note" >}}
While our naming scheme resembles [Semantic Versioning](https://semver.org/), at this time we do not strictly follow its
guidelines to the letter. Our goal is to provide regular releases that are as stable as possible, and we take backwards-compatibility
seriously. As with any software, always read the [release notes](https://grafana.com/docs/loki/<LOKI_VERSION>/release-notes/) and the [upgrade guide](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/) whenever
choosing a new version of Loki to install.
{{< /admonition >}}

New releases are based of a [weekly release](#weekly-releases) which we have vetted for stability over a number of weeks.

We strongly recommend keeping up-to-date with patch releases as they are released. We post updates of new releases in the `#loki` channel
of our [Slack community](https://grafana.com/docs/loki/<LOKI_VERSION>/community/getting-in-touch/).

You can find all of our releases [on GitHub](https://github.com/grafana/loki/releases) and on [Docker Hub](https://hub.docker.com/r/grafana/loki).

## Weekly Releases

Every Monday morning, we create a new "weekly" release from the tip of the [`main` branch](https://github.com/grafana/loki).
These releases use the following naming scheme:

<ul>
<code>weekly-k&lt;week-number&gt;</code> where <code>&lt;week-number&gt;</code> is the number of weeks since we began this process (2020-07-06).
</ul>

These weekly releases are deployed across our Grafana Cloud Logs fleet of instances. We test these releases for stability
by deploying them through development, pre-production, and production instances.

Generally these weekly releases are considered stable enough to run, but we provide zero stability guarantees and these
releases _should not be run in production_ unless you are willing to tolerate some risk.

You can find these releases on [Docker Hub](https://hub.docker.com/r/grafana/loki/tags?page=1&name=k).

### Which release will my merged PR be part of?

Once your PR is merged to `main`, you can expect it to become available in the next week's
[weekly release](#weekly-releases). To find out which stable or weekly releases a commit is included in, use the following tool:

`tools/which-release.sh`

For example, [this PR](https://github.com/grafana/loki/pull/7472) was [merged](https://github.com/grafana/loki/pull/7472#event-8431624850) into the commit named `d434e80`. Using the tool above, we can see that is part of release 2.8 and several weekly releases:

```bash
$ ./tools/which-release.sh d434e80                                 
Commit was found in the following releases:
  release-2.8.x
Commit was found in the following weekly builds:
  k136
  k137
  k138
  k139
  k140
  k141
  k142
```
