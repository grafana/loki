---
title: Prepare Major Release
description: Describes the process to create a workflow for a major release of Grafana Loki.
---
# Major Release

A major release follows the same process as [minor and patch releases]({{< relref "./prepare-release.md" >}}), but requires a custom workflow to be created to run on the branch we want to release from. The reason for this is that we don't do major releases very often, so it is not economical to keep those workflows running all the time.

To create a major release workflow, follow the steps below.

1. Edit `./github/release-workflows.jsonnet`
1. Add a new workflow for the major release. For example, the 3.0 release looked like the following:

```jsonnet
  'three-zero-release.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      branches=['release-3.0.0'],
      buildImage=buildImage,
      checkTemplate=checkTemplate,
      golangCiLintVersion=golangCiLintVersion,
      imageBuildTimeoutMin=imageBuildTimeoutMin,
      imageJobs=imageJobs,
      imagePrefix=imagePrefix,
      releaseLibRef=releaseLibRef,
      releaseRepo='grafana/loki',
      skipArm=false,
      skipValidation=false,
      useGitHubAppToken=true,
      releaseAs='3.0.0',
    ) + {
      name: 'Prepare Loki 3.0 release',
    }, false, false
  ),

```

1. Make sure the `branches` field is set to the release branch you want to release from.
1. Make sure the `releaseAs` field is set to the version you want to release.
1. Run `make release-workflows` to generate the new workflow. Merge this change to both the main and release branch.
