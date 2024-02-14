{
  common: import './workflows/common.libsonnet',
  build: import './workflows/build.libsonnet',
  release: import './workflows/release.libsonnet',
  validate: import './workflows/validate.libsonnet',
  releasePRWorkflow: function(
    branches=['release-[0-9]+.[0-9]+.x', 'k[0-9]+'],
    buildImage='grafana/loki-build-image:0.33.0',
    dockerUsername='grafana',
    imageJobs={},
    imagePrefix='grafana',
    releaseRepo='grafana/loki-release',
    skipValidation=false,
    skipArm=true,
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
}
