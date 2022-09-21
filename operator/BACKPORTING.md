# Backporting upstream PRs into an OpenShift Logging Release Branch

The following instructions describe how to backport a merged Loki-Operator upstream PR into an OpenShift Logging release branch.

## Background

OpenShift Logging release branches for the Loki Operator follow the product version scheme. In detail, each minor version of the OpenShift Logging 5 release stream yields an individual `release-5.y` release branch. For example, for OpenShift Logging `5.5.0 - 5.5.z` we expect that the bundled operators are built from a corresponding branch named `release-5.5`.

## Instructions

The following approach describes how to backport changes merged into the [grafana/loki][grafana-loki] `main` branch.

__Note:__ The grafana/loki upstream repository follows a squash-and-merge approach when merging PRs. This means each PR represents one commit in the `main` branch history. In case you had to create follow-up PRs for the same issue because of flaws or incompleteness identified after the merge, you are encouraged to combine them in a **single** backport PR.

### Ensure you have this repository as a local remote

```shell
$ git remote add openshift git@github.com:openshift/loki.git
```

### Identify the PR to backport

Use the upstream [changelog][loki-operator-changelog] file to identify the PR you need to backport, e.g. [grafana/loki#7000](https://github.com/grafana/loki/pull/7000).

### Create a local backport branch

```console
$ git checkout -b backport-pr-7000 openshift/release-5.5
```

### Cherry pick your PR into the backport branch

Open the backport candidate PR [grafana/loki#7000](https://github.com/grafana/loki/pull/7000) and scroll to bottom to identify the merge commit into `main`, e.g.:

```
periklis merged commit 766d4b7 into grafana:main 20 days ago
```

and cherry-pick:

```console
$ git cherry-pick 766d4b7
```

__Note:__ When backporting PRs from `main`, merge conflicts can arise because our upstream `main` history includes developed features and refactorings for the next minor release.


### Open a backport PR

When opening the backport PR ensure the following:

1. Select `openshift/loki` as **base repository** and `release-5.5` as **base branch**.
2. The title has the form: `[release-5.5] Backport PR grafana/loki#7000`
3. Add a description providing special notes for the reviewer and approver.
4. Provide a link to the backport issue from [Red Hat JIRA][redhat-jira]
5. *Optional:* Highlight any specific merge conflict resolutions for the reviewer.

Example PR:

```
Title: [release-5.5] Backport PR grafana/loki#7000

Description:

The present PR is a backport of the gateway alerting rules fixes. The tests provided here are a subset of the upstream PR. The dropped test cases depended on the new feature gate `UseServiceMonitorSafeTLS` which is targeted for the next minor release.

Ref: LOG-2895

/cc @xperimental
/assign @periklis
```


[grafana-loki]: https://github.com/grafana/loki
[loki-operator-changelog]: https://github.com/grafana/loki/blob/main/operator/CHANGELOG.md
[redhat-jira]: https://issues.redhat.com/browse/LOG
