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
    imageJobs={},
    imagePrefix='grafana',
    releaseRepo='grafana/loki-release',
    skipArm=true,
    skipValidation=false,
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
      RELEASE_REPO: releaseRepo,
      DOCKER_USERNAME: dockerUsername,
      IMAGE_PREFIX: imagePrefix,
      SKIP_VALIDATION: skipValidation,
      VERSIONING_STRATEGY: versioningStrategy,
    },
    local validationSteps = ['check'],
    jobs: {
      check: {} + $.job.withUses(checkTemplate)
             + $.job.with({
               skip_validation: skipValidation,
             }),
      version: $.build.version + $.common.job.withNeeds(validationSteps),
      dist: $.build.dist(buildImage, skipArm) + $.common.job.withNeeds(['version']),
    } + std.mapWithKey(function(name, job) job + $.common.job.withNeeds(['version']), imageJobs) + {
      local buildImageSteps = ['dist'] + std.objectFields(imageJobs),
      'create-release-pr': $.release.createReleasePR + $.common.job.withNeeds(buildImageSteps),
    },
  },
  releaseWorkflow: function(
    releaseRepo='grafana/loki-release',
    dockerUsername='grafana',
    imagePrefix='grafana',
    branches=['release-[0-9].[0-9].x', 'k[0-9]*'],
    getDockerCredsFromVault=false
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
    },
    jobs: {
      shouldRelease: $.release.shouldRelease,
      createRelease: $.release.createRelease,
      publishImages: $.release.publishImages(getDockerCredsFromVault, dockerUsername),
    },
  },
  check: function(
    buildImage='grafana/loki-build-image:0.33.0',
        ) {
    name: 'check',
    on: {
      workflow_call: {
        inputs: {
          skip_validation: {
            default: false,
            description: 'skip validation steps',
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
    jobs: $.validate(buildImage),
  },
}
