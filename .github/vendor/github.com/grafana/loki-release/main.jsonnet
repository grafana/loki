{
  common: import './workflows/common.libsonnet',
  build: import './workflows/build.libsonnet',
  release: import './workflows/release.libsonnet',
  validate: import './workflows/validate.libsonnet',
  releasePRWorkflow: function(
    branches=['release-[0-9].[0-9].x', 'k[0-9]*'],
    buildImage='grafana/loki-build-image:0.33.0',
    dockerUsername='grafana',
    imageJobs={},
    imagePrefix='grafana',
    releaseRepo='grafana/loki-release',
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
    local validationSteps = ['test', 'lint', 'check'],
    jobs: $.validate(buildImage) {
      dist: $.build.dist(buildImage) + $.common.job.withNeeds(validationSteps),
    } + std.mapWithKey(function(name, job) job + $.common.job.withNeeds(validationSteps), imageJobs) + {
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
    },
    concurrency: {
      group: 'create-release-${{ github.sha }}',
    },
    env: {
      RELEASE_REPO: releaseRepo,
      IMAGE_PREFIX: imagePrefix,
    } + if !getDockerCredsFromVault then {
      DOCKER_USERNAME: dockerUsername,
    } else {},
    jobs: {
      shouldRelease: $.release.shouldRelease,
      createRelease: $.release.createRelease,
      publishImages: $.release.publishImages(getDockerCredsFromVault),
    },
  },
}
