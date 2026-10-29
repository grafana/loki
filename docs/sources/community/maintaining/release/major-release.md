---
title: Prepare Major Release
description: Describes the process to create a workflow for a major release of Grafana Loki.
---

# Prepare Major Release

A major release follows the same process as [minor and patch releases](../prepare-release/), but requires a custom workflow to be created to run on the branch we want to release from. The reason for this is that we don't do major releases very often, so it is not economical to keep those workflows running all the time.

To create a major release workflow, follow the steps below.

1. Edit `./github/release-workflows.jsonnet`
1. Add a new workflow for the major release. For example, the 3.0 release looked like the following:

```jsonnet
  'major-release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      branches=['release-*.0.0'],
      buildImage=buildImage,
      checkTemplate=checkTemplate,
      distRunsOn='ubuntu-x64',
      distOptionalTargets=['dist/loki-linux-riscv64'],
      golangCiLintVersion=golangCiLintVersion,
      imageBuildTimeoutMin=imageBuildTimeoutMin,
      imageJobs=imageJobs,
      imagePrefix=imagePrefix,
      releaseLibRef=releaseLibRef,
      releaseRepo='grafana/loki',
      skipArm=false,
      skipValidation=false,
      // Instead of hard-coding the release version, push an empty to the target branch `release-X.0.0`
      // git commit --allow-empty -m "chore: Release X.0.0-rcY" -m "Release-As: X.0.0-rcY"
      // https://github.com/googleapis/release-please#how-do-i-change-the-version-number
      // releaseAs='x.0.0',
    ) + {
      name: 'Prepare Major Release PR',
    }, false, false
  ),

```

1. Make sure the `branches` field is set to the release branch you want to release from.
1. Make sure the `releaseAs` field is set to the version you want to release.
1. Run `make release-workflows` to generate the new workflow. Merge this change to both the main and release branch. You may also want to disable the patch release workflow as part of this PR, see below.

## Race Condition with Patch Release

Once this new workflow is created there is a bit of a race condition with the patch workflow. This can be resolved in 1 of 2 ways:

1. Disable the patch release workflow until after the major release is complete.
1. Watch all actions for the release branch (ie. <https://github.com/grafana/loki/actions?query=branch%3Arelease-3.0.x>) and cancel and patch release runs.
