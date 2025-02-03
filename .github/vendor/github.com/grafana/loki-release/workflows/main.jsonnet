{
  common: import 'common.libsonnet',
  job: $.common.job,
  step: $.common.step,
  build: import 'build.libsonnet',
  release: import 'release.libsonnet',
  validate: import 'validate.libsonnet',
  validateGel: import 'validate-gel.libsonnet',
  releasePRWorkflow: function(
    branches=['release-[0-9]+.[0-9]+.x', 'k[0-9]+'],
    buildArtifactsBucket='loki-build-artifacts',
    buildImage='grafana/loki-build-image:0.34.0',
    changelogPath='CHANGELOG.md',
    checkTemplate='./.github/workflows/check.yml',
    distMakeTargets=['dist', 'packages'],
    dryRun=false,
    dockerUsername='grafana',
    golangCiLintVersion='v1.60.3',
    imageBuildTimeoutMin=25,
    imageJobs={},
    imagePrefix='grafana',
    releaseAs=null,
    releaseLibRef='main',
    releaseRepo='grafana/loki-release',
    skipArm=false,
    skipValidation=false,
    useGitHubAppToken=true,
    useGCR=false,
    versioningStrategy='always-bump-patch',
                    ) {
    name: 'create release PR',
    on: {
      push: {
        branches: branches,
      },
    },
    permissions: {
      contents: 'write',
      'pull-requests': 'write',
      'id-token': 'write',
    },
    concurrency: {
      group: 'create-release-pr-${{ github.sha }}',
    },
    env: {
      BUILD_ARTIFACTS_BUCKET: buildArtifactsBucket,
      BUILD_TIMEOUT: imageBuildTimeoutMin,
      CHANGELOG_PATH: changelogPath,
      DOCKER_USERNAME: dockerUsername,
      DRY_RUN: dryRun,
      IMAGE_PREFIX: imagePrefix,
      RELEASE_LIB_REF: releaseLibRef,
      RELEASE_REPO: releaseRepo,
      SKIP_VALIDATION: skipValidation,
      USE_GITHUB_APP_TOKEN: useGitHubAppToken,
      VERSIONING_STRATEGY: versioningStrategy,
    } + if releaseAs != null then {
      RELEASE_AS: releaseAs,
    } else {},
    local validationSteps = ['check'],
    jobs: {
      check: {} + $.job.withUses(checkTemplate)
             + $.job.with({
               skip_validation: skipValidation,
               build_image: buildImage,
               golang_ci_lint_version: golangCiLintVersion,
               release_lib_ref: releaseLibRef,
               use_github_app_token: useGitHubAppToken,
             })
             + if useGCR then $.job.withSecrets({
               GCS_SERVICE_ACCOUNT_KEY: '${{ secrets.GCS_SERVICE_ACCOUNT_KEY }}',
             }) else {},
      version: $.build.version + $.common.job.withNeeds(validationSteps),
      dist: $.build.dist(buildImage, skipArm, useGCR, distMakeTargets) + $.common.job.withNeeds(['version']),
    } + std.mapWithKey(function(name, job) job + $.common.job.withNeeds(['version']), imageJobs) + {
      local buildImageSteps = ['dist'] + std.objectFields(imageJobs),
      'create-release-pr': $.release.createReleasePR + $.common.job.withNeeds(buildImageSteps),
    },
  },
  releaseWorkflow: function(
    branches=['release-[0-9].[0-9].x', 'k[0-9]*'],
    buildArtifactsBucket='loki-build-artifacts',
    dockerUsername='grafanabot',
    getDockerCredsFromVault=false,
    imagePrefix='grafana',
    pluginBuildDir='release/plugin-tmp-dir',
    publishBucket='',
    publishToGCS=false,
    releaseLibRef='main',
    releaseRepo='grafana/loki-release',
    useGitHubAppToken=true,
    dockerPluginPath='clients/cmd/docker-driver',
                  ) {
    name: 'create release',
    on: {
      push: {
        branches: branches,
      },
    },
    permissions: {
      contents: 'write',
      'pull-requests': 'write',
      'id-token': 'write',
    },
    concurrency: {
      group: 'create-release-${{ github.sha }}',
    },
    env: {
      BUILD_ARTIFACTS_BUCKET: buildArtifactsBucket,
      IMAGE_PREFIX: imagePrefix,
      RELEASE_LIB_REF: releaseLibRef,
      RELEASE_REPO: releaseRepo,
      USE_GITHUB_APP_TOKEN: useGitHubAppToken,
    } + if publishToGCS then {
      PUBLISH_BUCKET: publishBucket,
      PUBLISH_TO_GCS: true,
    } else {
      PUBLISH_TO_GCS: false,
    },
    jobs: {
      shouldRelease: $.release.shouldRelease,
      createRelease: $.release.createRelease,
      publishImages: $.release.publishImages(getDockerCredsFromVault, dockerUsername),
      publishDockerPlugins: $.release.publishDockerPlugins(pluginBuildDir, getDockerCredsFromVault, dockerUsername),
      publishRelease: $.release.publishRelease(['createRelease', 'publishImages', 'publishDockerPlugins']),
    },
  },
  check: {
    name: 'check',
    on: {
      workflow_call: {
        inputs: {
          build_image: {
            description: 'loki build image to use',
            required: true,
            type: 'string',
          },
          skip_validation: {
            default: false,
            description: 'skip validation steps',
            required: false,
            type: 'boolean',
          },
          golang_ci_lint_version: {
            default: 'v1.60.3',
            description: 'version of golangci-lint to use',
            required: false,
            type: 'string',
          },
          release_lib_ref: {
            default: 'main',
            description: 'git ref of release library to use',
            required: false,
            type: 'string',
          },
          use_github_app_token: {
            default: true,
            description: 'whether to use the GitHub App token for GH_TOKEN secret',
            required: false,
            type: 'boolean',
          },
        },
      },
    },
    permissions: {
      contents: 'write',
      'pull-requests': 'write',
      'id-token': 'write',
    },
    concurrency: {
      group: 'check-${{ github.sha }}',
    },
    env: {
      RELEASE_LIB_REF: '${{ inputs.release_lib_ref }}',
      USE_GITHUB_APP_TOKEN: '${{ inputs.use_github_app_token }}',
    },
    jobs: $.validate,
  },
  checkGel: {
    name: 'check',
    on: {
      workflow_call: {
        inputs: {
          build_image: {
            description: 'loki build image to use',
            required: true,
            type: 'string',
          },
          skip_validation: {
            default: false,
            description: 'skip validation steps',
            required: false,
            type: 'boolean',
          },
          golang_ci_lint_version: {
            default: 'v1.60.3',
            description: 'version of golangci-lint to use',
            required: false,
            type: 'string',
          },
          release_lib_ref: {
            default: 'main',
            description: 'git ref of release library to use',
            required: false,
            type: 'string',
          },
          use_github_app_token: {
            default: true,
            description: 'whether to use the GitHub App token for GH_TOKEN secret',
            required: false,
            type: 'boolean',
          },
        },
        secrets: {
          GCS_SERVICE_ACCOUNT_KEY: {
            description: 'GCS service account key',
            required: true,
          },
        },
      },
    },
    permissions: {
      contents: 'write',
      'pull-requests': 'write',
      'id-token': 'write',
    },
    concurrency: {
      group: 'check-${{ github.sha }}',
    },
    env: {
      RELEASE_LIB_REF: '${{ inputs.release_lib_ref }}',
      USE_GITHUB_APP_TOKEN: '${{ inputs.use_github_app_token }}',
    },
    jobs: $.validateGel,
  },
}
