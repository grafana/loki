local lokiRelease = import 'workflows/main.jsonnet';
local build = lokiRelease.build;
local job = lokiRelease.job;

local releaseLibRef = std.filter(
  function(dep) dep.source.git.remote == 'https://github.com/grafana/loki-release.git',
  (import 'jsonnetfile.json').dependencies
)[0].version;

local checkTemplate = 'grafana/loki-release/.github/workflows/check.yml@%s' % releaseLibRef;

local imageJobs = {
  loki: build.image('loki', 'cmd/loki'),
  fluentd: build.image('fluent-plugin-loki', 'clients/cmd/fluentd', platform=['linux/amd64']),
  'fluent-bit': build.image('fluent-bit-plugin-loki', 'clients/cmd/fluent-bit', platform=['linux/amd64']),
  logstash: build.image('logstash-output-loki', 'clients/cmd/logstash', platform=['linux/amd64']),
  logcli: build.image('logcli', 'cmd/logcli'),
  'loki-canary': build.image('loki-canary', 'cmd/loki-canary'),
  'loki-operator': build.image('loki-operator', 'operator', context='release/operator', platform=['linux/amd64']),
  promtail: build.image('promtail', 'clients/cmd/promtail'),
  querytee: build.image('loki-query-tee', 'cmd/querytee', platform=['linux/amd64']),
};

local buildImage = 'grafana/loki-build-image:0.30.1';
local golangCiLintVersion = 'v1.51.2';

{
  'patch-release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      imageJobs=imageJobs,
      buildImage=buildImage,
      branches=['release-[0-9]+.[0-9]+.x'],
      checkTemplate=checkTemplate,
      golangCiLintVersion=golangCiLintVersion,
      imagePrefix='grafana',
      releaseLibRef=releaseLibRef,
      releaseRepo='grafana/loki',
      skipArm=false,
      skipValidation=false,
      versioningStrategy='always-bump-patch',
      useGitHubAppToken=true,
    ), false, false
  ),
  'minor-release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      imageJobs=imageJobs,
      buildImage=buildImage,
      branches=['k[0-9]+'],
      checkTemplate=checkTemplate,
      golangCiLintVersion=golangCiLintVersion,
      imagePrefix='grafana',
      releaseLibRef=releaseLibRef,
      releaseRepo='grafana/loki',
      skipArm=false,
      skipValidation=false,
      versioningStrategy='always-bump-minor',
      useGitHubAppToken=true,
    ), false, false
  ),
  'release.yml': std.manifestYamlDoc(
    lokiRelease.releaseWorkflow(
      branches=['release-[0-9]+.[0-9]+.x', 'k[0-9]+'],
      getDockerCredsFromVault=true,
      imagePrefix='grafana',
      releaseLibRef=releaseLibRef,
      releaseRepo='grafana/loki',
      useGitHubAppToken=false,
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
      check: job.new()
             + job.withUses('grafana/loki-release/.github/workflows/check.yml@%s' % releaseLibRef)
             + job.with({
               build_image: buildImage,
               golang_ci_lint_version: golangCiLintVersion,
               release_lib_ref: releaseLibRef,
               skip_validation: false,
               use_github_app_token: true,
             }),
    },
  }),
}
