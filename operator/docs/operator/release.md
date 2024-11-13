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

This document will go over the design of the release process for the Loki Operator and how to release it.

## Design

To release Loki Operator we need the following:

1. Bump the Loki Operator version and generate the bundle manifests with `make bundle-all`;
2. Update the CHANGELOG.md with the new version;
3. Create a release tag and a release on GitHub;
4. Open two PRs to [k8s-operatorhub/community-operators](https://github.com/k8s-operatorhub/community-operators) and [redhat-openshift-ecosystem/community-operators-prod](https://github.com/redhat-openshift-ecosystem/community-operators-prod) with the contents of the new bundles;

Loki Operator uses the GitHub [action release-please](https://github.com/google-github-actions/release-please-action) to automate steps 2 and 3. Furthermore, to automate step 4 we use a workflow that is triggered when a release tag is created.

In the following sections, we will go over how the workflows are configured.

### release-please

release-please automates CHANGELOG generation, the creation of GitHub releases, and version bumps. It does so by parsing the git history, looking for Conventional Commit messages, and creating release PRs. Once a release PR is merged release-please will create the release and it will again wait for a releasable unit before opening the next release PR. A releasable unit is a commit with one of the following prefixes: "feat", "fix", and "deps".

The workflow that is responsible for the operator release-please is `.github/workflows/operator-release-please.yml`. Note that the operator release-please process is different from the one used by Loki. The operator release-please configuration lives in `operator/release-please-config.json`.

Useful links:

- release-please [customizing releases documentation](https://github.com/googleapis/release-please/blob/main/docs/customizing.md)
- release-please [config documentation](https://github.com/googleapis/release-please/blob/main/docs/manifest-releaser.md#configfile)

The following sub-section contains some notes on the Loki operator release-please configuration:

- Use of `bump-minor-pre-major` and `bump-patch-for-minor-pre-major`;
- Use of `draft`;
- Preventing merging the release-please PR without updating the manifests;

#### Use of `bump-minor-pre-major` and `bump-patch-for-minor-pre-major`

Since the operator is still pre `v1.0.0` we are leveraging `bump-minor-pre-major` and `bump-patch-for-minor-pre-major` so that merging "feat", "fix", and "deps" commits will only bump a patch version and merging "feat!" and "fix!" will bump the minor version.

As of writing, the operator release-please will only act on merges to `main`. This means that we can support the following release scenarios:

- Case 1: Release a patch version of v0.Y.x+1 with the diff from v0.Y.x. This is only supported until a breaking feature gets merged to `main`.
- Case 2: Release a new minor version v0.Y+1.0 with the diff from v0.Y.x

#### Use of `draft`

Since the operator shares the same repo with Loki, we want to make sure that, when we create a release of the operator we don't that release to `latest`, otherwise it would look like the latest release from the operator was Loki's latest release. Unfortunately, release-please doesn't provide a way to disable this, so instead we enable `draft`. `draft` makes it so releases created by release-please are only created in draft. We then use a step that will publish the release without setting it to the latest.

#### Preventing merging the release-please PR without updating the manifests

Since step 1. is currently not automated and disconnected from release-please we have put in place a workflow in `.github/workflows/operator-check-prepare-release-commit.yml` that runs on release-please PRs. This workflow is responsible for making sure that in master exists a commit with the message `chore(operator): prepare community release v$VERSION`. Once we automate step 1. we should be able to remove this workflow.

### Publish release to operatorhubs

To publish a community release of Loki Operator to the community hubs we leverage the workflow in `.github/workflows/operator-publish-operator-hub.yml` this workflow is set to trigger on tag creation that matches `operator/`.

This workflow will then use a workflow `.github/workflows/operator-reusable-hub-release.yml` that's responsible for:

- Creating on the folder `operators/loki-operator/` a new folder with the manifests for the new version;
- Adding the ocp supported version annotation to the `metadata.yaml` file only in the OpenShift community repo;
- Creating a PR for the appropriate community repo.

## Releasing

1. Create a PR to bump the version (i.e [v0.6.1 preparation PR](https://github.com/grafana/loki/pull/13105)), be careful with the commit message;
2. Re-trigger the action `operator-publish-operator-hub` on the release-please PR;
3. Merge the release-please PR (i.e [v0.6.1 release PR](https://github.com/grafana/loki/pull/12593) );
4. Grafana bot will build and push the release images to `grafana/loki-operator` ([docker repo](https://hub.docker.com/r/grafana/loki-operator/tags));
5. Grafana bot will automatically open a PRs to [k8s-operatorhub/community-operators](https://github.com/k8s-operatorhub/community-operators) and [redhat-openshift-ecosystem/community-operators-prod](https://github.com/redhat-openshift-ecosystem/community-operators-prod);
