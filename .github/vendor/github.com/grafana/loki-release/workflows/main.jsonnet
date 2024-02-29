{
  common: import 'common.libsonnet',
  job: $.common.job,
  step: $.common.step,
  build: import 'build.libsonnet',
  release: import 'release.libsonnet',
  validate: import 'validate.libsonnet',
  releasePRWorkflow: function(
    branches=['release-[0-9]+.[0-9]+.x', 'k[0-9]+'],
    buildImage='grafana/loki-build-image:0.33.0',
    checkTemplate='./.github/workflows/check.yml',
    dockerUsername='grafana',
    golangCiLintVersion='v1.55.1',
    imageJobs={},
    imagePrefix='grafana',
    releaseLibRef='main',
    releaseRepo='grafana/loki-release',
    skipArm=false,
    skipValidation=false,
    versioningStrategy='always-bump-patch',
    useGitHubAppToken=true,
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
      RELEASE_REPO: releaseRepo,
      DOCKER_USERNAME: dockerUsername,
      IMAGE_PREFIX: imagePrefix,
      SKIP_VALIDATION: skipValidation,
      VERSIONING_STRATEGY: versioningStrategy,
      RELEASE_LIB_REF: releaseLibRef,
      USE_GITHUB_APP_TOKEN: useGitHubAppToken,
    },
    local validationSteps = ['check'],
    jobs: {
      check: {} + $.job.withUses(checkTemplate)
             + $.job.with({
               skip_validation: skipValidation,
               build_image: buildImage,
               golang_ci_lint_version: golangCiLintVersion,
               release_lib_ref: releaseLibRef,
               use_github_app_token: useGitHubAppToken,
             }),
      version: $.build.version + $.common.job.withNeeds(validationSteps),
      dist: $.build.dist(buildImage, skipArm) + $.common.job.withNeeds(['version']),
    } + std.mapWithKey(function(name, job) job + $.common.job.withNeeds(['version']), imageJobs) + {
      local buildImageSteps = ['dist'] + std.objectFields(imageJobs),
      'create-release-pr': $.release.createReleasePR + $.common.job.withNeeds(buildImageSteps),
    },
  },
  releaseWorkflow: function(
    branches=['release-[0-9].[0-9].x', 'k[0-9]*'],
    dockerUsername='grafana',
    getDockerCredsFromVault=false,
    imagePrefix='grafana',
    releaseLibRef='main',
    releaseRepo='grafana/loki-release',
    useGitHubAppToken=true,
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
      RELEASE_REPO: releaseRepo,
      IMAGE_PREFIX: imagePrefix,
      RELEASE_LIB_REF: releaseLibRef,
      USE_GITHUB_APP_TOKEN: useGitHubAppToken,
    },
    jobs: {
      shouldRelease: $.release.shouldRelease,
      createRelease: $.release.createRelease,
      publishImages: $.release.publishImages(getDockerCredsFromVault, dockerUsername),
      publishRelease: $.release.publishRelease,
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
            default: 'v1.55.1',
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
}
