## Background

The Loki team currently releases versions of its software without an advertised schedule.

*Release cadences* are used by many large software projects to provide users and operators with a schedule which they can plan around. It also gives contributors an indication of how long it will take for their contributions to become publicly available.

## Scope

This RFC relates to Loki OSS public releases.

See the [Appendix](#Appendix) for the historical effective release cadence.

## Terminology

*VERSION* and *RELEASE* are used interchangeably to refer to a publicly-available, downloadable artifact with a version number. These artifacts are available under https://github.com/grafana/loki/releases and https://hub.docker.com/r/grafana/loki.

## Goals

The goals of this RFC are to:

- get community input on our proposed release cadence
- describe how we handle old releases (backporting)

## Proposed Release Cadence

The below are our proposals for the target times between releases; we might release sooner if the severity warrants it. We will endeavour to not miss these targets where practical.

- **[PATCH](https://semver.org/#spec-item-6)** releases: every **2 weeks**
- **[MINOR](https://semver.org/#spec-item-7)** releases: around **once a quarter**
- **[MAJOR](https://semver.org/#spec-item-8)** releases: no predefined schedule, likely every **12-18 months**

Adherence to the Semantic Versioning specification is out of scope for this RFC.

## Backporting

We can't expect all of our users to be running the latest release of Loki OSS at all times. As such, we want to propose a backporting strategy.

For bugfixes that are backportable, we will endeavour to backport these to the **last 2 minor releases**.

By way of example:

- Loki v15.0.0 is released, featuring a time machine
- Loki v15.1.0 is released, adding an "on/off" switch for the time machine (minor release)
- Loki v16.0.0 is released, featuring other cool stuff
- Loki v15.1.1 and v16.0.1 are released: a critical patch was added which fixes the "on/off" switch - allowing it to be flipped multiple times

We will backport the patch to v15.1.0 and v16.0.0 as the feature exists there, but not to v15.0.0 (can't fix a switch which isn't there).

_How do we define backportable bugfixes?_ We've tried to come up with a clean definition, but it's quite a difficult thing to define; one exception here is security-related bugfixes, which should always be backported if relevant. We will use our best judgment, and contributors who submit bugfixes can of course motivate for their PRs to be backported.

## Pre-releases

We produce weekly container images available at https://hub.docker.com/r/grafana/loki/tags?page=1&name=k. We number these
releases using the number of weeks since we began producing these releases.

At the time of writing we are 89 weeks in, so the version is `k89`, and the latest image is `k89-d320873`.
`d320873` is the commit SHA that the image was built from, in a branch of the same name as the version:
https://github.com/grafana/loki/tree/k89.

Every week on a Monday morning, we have a CI job which "cuts" a new release by forking the tip of `main` into a new `kXX` branch.
This branch is not updated unless we find any issues with this image, in which case we will patch the release. Each time
we patch a release, we update the `kXX` branch, and subsequently a new image will be built with a _new commit SHA but
the same version_.

**When we publish a release, we take the latest `kXX` build which has proven stable and base a release off of that.**

As such, you can consider these weekly releases as "pre-release" quality, however please note that these images have no
stability guarantees right now. Their use is discouraged in production systems; you should only use the latest stable release
in production.

## Appendix

### Historical Effective Release Cadence

| Release Date | Version | Type  | Delta        |
|--------------|---------|-------|--------------|
| 12-01-2022   | 2.4.2   | patch | *+ 6 weeks*  |
| 08-11-2021   | 2.4.1   | patch | *+ 2 days*   |
| 06-11-2021   | 2.4.0   | minor | *+ 3 months* |
| 06-08-2021   | 2.3.0   | minor | *+ 4 months* |
| 06-04-2021   | 2.2.1   | patch | *+ 5 weeks*  |
| 11-03-2021   | 2.2.0   | minor | *+ 3 months* |
| 24-12-2020   | 2.1.0   | minor | *+ 1 day*    |
| 23-12-2020   | 2.0.1   | patch | *+ 2 months* |
| 26-10-2020   | 2.0.0   | major |              |
| ...          | ...     | ...   | ...          |