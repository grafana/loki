local lokiRelease = import 'workflows/main.jsonnet',
      job = lokiRelease.job,
      step = lokiRelease.step,
      build = lokiRelease.build;
local releaseLibRef = 'main';
local checkTemplate = 'grafana/loki-release/.github/workflows/check.yml@%s' % releaseLibRef;
local buildImageVersion = std.extVar('BUILD_IMAGE_VERSION');
local goVersion = std.extVar('GO_VERSION');
local buildImage = 'grafana/loki-build-image:%s' % buildImageVersion;
local golangCiLintVersion = 'v1.60.3';
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
  'loki-docker-driver': build.dockerPlugin('loki-docker-driver', dockerPluginDir, buildImage=buildImage, platform=platforms.all),
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
      'id-token': 'write',
      contents: 'write',
      'pull-requests': 'write',
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
        job.new()
        + job.withNeeds(['%s-image' % name])
        + job.withEnv({
          BUILD_TIMEOUT: imageBuildTimeoutMin,
        })
        + job.withSteps([
          step.new('Set up Docker buildx', 'docker/setup-buildx-action@v3'),
          step.new('Login to DockerHub (from Vault)', 'grafana/shared-workflows/actions/dockerhub-login@main'),
          step.new('Publish multi-arch manifest')
          + step.withRun(|||
            # Unfortunately there is no better way atm than having a separate named output for each digest
            echo 'linux/arm64 ${{ needs.%(name)s.outputs.image_digest_linux_amd64 }}'
            echo 'linux/amd64 ${{ needs.%(name)s.outputs.image_digest_linux_arm64 }}'
            echo 'linux/arm   ${{ needs.%(name)s.outputs.image_digest_linux_arm }}'
            IMAGE=${{ needs.%(name)s.outputs.image_name }}:${{ needs.%(name)s.outputs.image_tag }}
            echo "Create multi-arch manifest for $IMAGE"
            docker buildx imagetools create -t $IMAGE \
              ${{ needs.%(name)s.outputs.image_name }}@${{ needs.%(name)s.outputs.image_digest_linux_amd64 }} \
              ${{ needs.%(name)s.outputs.image_name }}@${{ needs.%(name)s.outputs.image_digest_linux_arm64 }} \
              ${{ needs.%(name)s.outputs.image_name }}@${{ needs.%(name)s.outputs.image_digest_linux_arm }}
            docker buildx imagetools inspect $IMAGE
          ||| % { name: '%s-image' % name }),
        ])
      for name in std.objectFields(weeklyImageJobs)
    },
  }),
}
