---
title: "Release operations"
description: "Documentation on how the operator release process works"
lead: ""
date: 2023-04-01T09:00:00+00:00
lastmod: 2023-04-01T09:00:00+00:00
draft: false
images: []
menu:
  docs:
    parent: "operator"
weight: 100
toc: true
---

This document will go over the design of the release process for the Loki Operator and how to actually release.

# Design

To release Loki Operator we need the following:
1. Bump the Loki Operator version and generate the bundle manifests with `make bundle-all`;
2. Update the CHANGELOG.md with the new version;
3. Create a release tag on GitHub;
4. Open two PRs to [k8s-operatorhub/community-operators](https://github.com/k8s-operatorhub/community-operators) and [redhat-openshift-ecosystem/community-operators-prod](https://github.com/redhat-openshift-ecosystem/community-operators-prod) with the contents of the new bundles;

Loki Operator uses the GitHub [action release-please](https://github.com/google-github-actions/release-please-action) to automate steps 2 and 3. Furthermore to automate step 4 we use a workflow that is triggered when a release tag is created.

In the following sections we will go over how the workflows are configured.

## release-please

release-please automates CHANGELOG generation, the creation of GitHub releases, and version bumps. It does so by parsing the git history, looking for Conventional Commit messages, and creating release PRs. Once a release PR is merged release-please will create the release and it will again wait for a releasable unit before opening the next release PR. A releasable unit is a commit with one of the following prefixes: "feat", "fix", and "deps".

The workflow that is responsible for the operator release-please is `.github/workflows/operator-release-please.yml`. Note that the operator release-please process is different from the one used by Loki. The operator release-please configuration lives in `operator/release-please-config.json`.

Useful links:
- release-please [customizing releases documentation](https://github.com/googleapis/release-please/blob/main/docs/customizing.md)
- release-please [config documentation](https://github.com/googleapis/release-please/blob/main/docs/manifest-releaser.md#configfile)

Since the operator is still pre `v1.0.0` we are leveraging `bump-minor-pre-major` and `bump-patch-for-minor-pre-major` so that merging "feat", "fix", and "deps" commits will only bump a patch version and merging "feat!" and "fix!" will bump the minor version.

As of writting, the operator release-please will only act on merges to `main`. This means that we are able to support the following release scenarios:
- Case 1: Release a patch version of v0.Y.x+1 with the diff from v0.Y.x. This is only supported until a breaking feature gets merged to `main`.
- Case 2: Release a new minor version v0.Y+1.0 with the diff from v0.Y.x

## Publish release to operatorhubs

To publish a community release of Loki Operator to the community hubs we leverage the workflow in `.github/workflows/operator-publish-operator-hub.yml` this workflow is set to trigger on tag creation that matches `operator/`.

This workflow will then use a workflow `.github/workflows/operator-reusable-hub-release.yml` that's responsible for:
- Creating on the folder `operators/loki-operator/` a new folder with the manifests for the new version;
- Adding the ocp supported version annotation to the `metadata.yaml` file only in the OpenShift community repo;
- Creating a PR to the appropriate community repo.

# Releasing

1. Creating a PR to bump the version (i.e https://github.com/grafana/loki/pull/12246)
2. Merging the release-please PR (i.e TBD )
3. Grafana bot will automatically open a PRs to [k8s-operatorhub/community-operators](https://github.com/k8s-operatorhub/community-operators) and [redhat-openshift-ecosystem/community-operators-prod](https://github.com/redhat-openshift-ecosystem/community-operators-prod)

