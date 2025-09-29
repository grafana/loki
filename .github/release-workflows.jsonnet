local lokiRelease = import 'workflows/main.jsonnet',
      job = lokiRelease.job,
      step = lokiRelease.step,
      build = lokiRelease.build;
local releaseLibRef = (import 'jsonnetfile.json').dependencies[0].version;
local checkTemplate = 'grafana/loki-release/.github/workflows/check.yml@%s' % releaseLibRef;
local buildImageVersion = std.extVar('BUILD_IMAGE_VERSION');
local goVersion = std.extVar('GO_VERSION');
local buildImage = 'grafana/loki-build-image:%s' % buildImageVersion;
local golangCiLintVersion = 'v2.3.0';
local imageBuildTimeoutMin = 60;
local imagePrefix = 'grafana';
local dockerPluginDir = 'clients/cmd/docker-driver';
local runner = import 'workflows/runner.libsonnet',
      r = runner.withDefaultMapping();  // Do we need a different mapping?

local platforms = {
  amd: [r.forPlatform('linux/amd64')],
  arm: [r.forPlatform('linux/arm64'), r.forPlatform('linux/arm')],
  all: self.amd + self.arm,
};

local imageJobs = {
  loki: build.image('loki', 'cmd/loki', platform=platforms.all),
  fluentd: build.image('fluent-plugin-loki', 'clients/cmd/fluentd', platform=platforms.amd),
  'fluent-bit': build.image('fluent-bit-plugin-loki', 'clients/cmd/fluent-bit', platform=platforms.amd),
  logstash: build.image('logstash-output-loki', 'clients/cmd/logstash', platform=platforms.amd),
  logcli: build.image('logcli', 'cmd/logcli', platform=platforms.all),
  'loki-canary': build.image('loki-canary', 'cmd/loki-canary', platform=platforms.all),
  'loki-canary-boringcrypto': build.image('loki-canary-boringcrypto', 'cmd/loki-canary-boringcrypto', platform=platforms.all),
  promtail: build.image('promtail', 'clients/cmd/promtail', platform=platforms.all),
  querytee: build.image('loki-query-tee', 'cmd/querytee', platform=platforms.amd),
  'loki-docker-driver': build.dockerPlugin('loki-docker-driver', dockerPluginDir, buildImage=buildImage, platform=[r.forPlatform('linux/amd64'), r.forPlatform('linux/arm64')]),
};

local weeklyImageJobs = {
  loki: build.weeklyImage('loki', 'cmd/loki', platform=platforms.all),
  'loki-canary': build.weeklyImage('loki-canary', 'cmd/loki-canary', platform=platforms.all),
  'loki-canary-boringcrypto': build.weeklyImage('loki-canary-boringcrypto', 'cmd/loki-canary-boringcrypto', platform=platforms.all),
  promtail: build.weeklyImage('promtail', 'clients/cmd/promtail', platform=platforms.all),
};

{
  'patch-release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      branches=['release-[0-9]+.[0-9]+.x'],
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
      versioningStrategy='always-bump-patch',
    ) + {
      name: 'Prepare Patch Release PR',
    }, false, false
  ),
  'minor-release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      branches=['k[0-9]+'],
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
      versioningStrategy='always-bump-minor',
    ) + {
      name: 'Prepare Minor Release PR from Weekly',
    }, false, false
  ),
  'release.yml': std.manifestYamlDoc(
    lokiRelease.releaseWorkflow(
      branches=['release-[0-9]+.[0-9]+.x', 'k[0-9]+', 'main'],
      getDockerCredsFromVault=true,
      imagePrefix='grafana',
      releaseLibRef=releaseLibRef,
      pluginBuildDir=dockerPluginDir,
      releaseBranchTemplate='release-\\${major}.\\${minor}.x',
      releaseRepo='grafana/loki',
      useGitHubAppToken=true,
    ), false, false
  ),
  'check.yml': std.manifestYamlDoc({
    name: 'check',
    on: {
      pull_request: {},
      push: {
        branches: ['main'],
      },
    },
    permissions: {
      contents: 'read',
      'pull-requests': 'read',
    },
    jobs: {
      check: {
        uses: checkTemplate,
        with: {
          build_image: buildImage,
          golang_ci_lint_version: golangCiLintVersion,
          release_lib_ref: releaseLibRef,
          skip_validation: false,
          use_github_app_token: true,
        },
      },
    },
  }),
  'images.yml': std.manifestYamlDoc({
    name: 'Publish images',
    on: {
      push: {
        branches: [
          'k[0-9]+*',  // This is a weird glob pattern, not a regexp, do not use ".*", see https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet
          'main',
        ],
      },
      workflow_dispatch: {},
    },
    permissions: {
      contents: 'read',
      'pull-requests': 'read',
    },
    jobs: {
      check: {
        uses: checkTemplate,
        with: {
          build_image: buildImage,
          golang_ci_lint_version: golangCiLintVersion,
          release_lib_ref: releaseLibRef,
          skip_validation: false,
          use_github_app_token: true,
        },
      },
    } + {
      ['%s-image' % name]:
        weeklyImageJobs[name]
        + job.withNeeds(['check'])
        + job.withPermissions({
          contents: 'read',
          'id-token': 'write',
        })
        + job.withEnv({
          BUILD_TIMEOUT: imageBuildTimeoutMin,
          RELEASE_REPO: 'grafana/loki',
          RELEASE_LIB_REF: releaseLibRef,
          IMAGE_PREFIX: imagePrefix,
          GO_VERSION: goVersion,
        })
      for name in std.objectFields(weeklyImageJobs)
    } + {
      ['%s-manifest' % name]:
        job.new() +
        job.withPermissions({
          contents: 'read',
          'id-token': 'write',
        })
        + job.withNeeds(['%s-image' % name])
        + job.withEnv({
          BUILD_TIMEOUT: imageBuildTimeoutMin,
          IMAGE_DIGEST_AMD64: '${{ needs.%(name)s-image.outputs.image_digest_linux_amd64 }}' % name,
          IMAGE_DIGEST_ARM64: '${{ needs.%(name)s-image.outputs.image_digest_linux_arm64 }}' % name,
          IMAGE_DIGEST_ARM: '${{ needs.%(name)s-image.outputs.image_digest_linux_arm }}' % name,
          OUTPUTS_IMAGE_NAME: '${{ needs.%(name)s-image.outputs.image_name }}' % name,
          OUTPUTS_IMAGE_TAG: '${{ needs.%(name)s-image.outputs.image_tag }}' % name,
        })
        + job.withSteps([
          step.new('Set up Docker buildx', 'docker/setup-buildx-action@b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2'),  // v3
          step.new('Login to DockerHub (from Vault)', 'grafana/shared-workflows/actions/dockerhub-login@75804962c1ba608148988c1e2dc35fbb0ee21746'),  // main
          step.new('Publish multi-arch manifest')
          + step.withRun(|||
            # Unfortunately there is no better way atm than having a separate named output for each digest
            echo "linux/arm64 $IMAGE_DIGEST_ARM64"
            echo "linux/amd64 $IMAGE_DIGEST_AMD64"
            echo "linux/arm   $IMAGE_DIGEST_ARM"
            IMAGE="${OUTPUTS_IMAGE_NAME}:${OUTPUTS_IMAGE_TAG}"
            echo "Create multi-arch manifest for $IMAGE"
            docker buildx imagetools create -t $IMAGE \
              ${OUTPUTS_IMAGE_NAME}@${IMAGE_DIGEST_ARM64} \
              ${OUTPUTS_IMAGE_NAME}@${IMAGE_DIGEST_AMD64} \
              ${OUTPUTS_IMAGE_NAME}@${IMAGE_DIGEST_ARM}
            docker buildx imagetools inspect $IMAGE
          ||| % { name: '%s-image' % name }),
        ])
      for name in std.objectFields(weeklyImageJobs)
    },
  }),
}
