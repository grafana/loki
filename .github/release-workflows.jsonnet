local lokiRelease = import 'loki-release/main.jsonnet';
local build = lokiRelease.build;
{
  'patch-release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      imageJobs={
        loki: build.image('loki', 'cmd/loki'),
        fluentd: build.image('fluentd', 'clients/cmd/fluentd', platform=['linux/amd64']),
        'fluent-bit': build.image('fluent-bit', 'clients/cmd/fluent-bit', platform=['linux/amd64']),
        logstash: build.image('logstash', 'clients/cmd/logstash', platform=['linux/amd64']),
        logcli: build.image('logcli', 'cmd/logcli'),
        'loki-canary': build.image('loki-canary', 'cmd/loki-canary'),
        'loki-canary-boringcrypto': build.image('loki-canary-boringcrypto', 'cmd/loki-canary-boringcrypto'),
        'loki-operator': build.image('loki-operator', 'operator', context='release/operator', platform=['linux/amd64']),
        promtail: build.image('promtail', 'clients/cmd/promtail'),
        querytee: build.image('querytee', 'cmd/querytee', platform=['linux/amd64']),
      },
      releaseRepo='grafana/loki',
      skipValidation=true,
      versioningStrategy='always-bump-patch',
      imagePrefix='grafana',
      // TODO: remove temp PR branch
      branches=['release-[0-9].[0-9].x', 'fix-action-names-in-main'],
    ), false, false
  ),
  'minor-release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      imageJobs={
        loki: build.image('loki', 'cmd/loki'),
        fluentd: build.image('fluentd', 'clients/cmd/fluentd', platform=['linux/amd64']),
        'fluent-bit': build.image('fluent-bit', 'clients/cmd/fluent-bit', platform=['linux/amd64']),
        logstash: build.image('logstash', 'clients/cmd/logstash', platform=['linux/amd64']),
        logcli: build.image('logcli', 'cmd/logcli'),
        'loki-canary': build.image('loki-canary', 'cmd/loki-canary'),
        'loki-canary-boringcrypto': build.image('loki-canary-boringcrypto', 'cmd/loki-canary-boringcrypto'),
        'loki-operator': build.image('loki-operator', 'operator', context='release/operator', platform=['linux/amd64']),
        promtail: build.image('promtail', 'clients/cmd/promtail'),
        querytee: build.image('querytee', 'cmd/querytee', platform=['linux/amd64']),
      },
      releaseRepo='grafana/loki',
      skipValidation=false,
      versioningStrategy='always-bump-minor',
      imagePrefix='grafana',
      branches=['k[0-9]*'],
    ), false, false
  ),
  'release.yml': std.manifestYamlDoc(
    lokiRelease.releaseWorkflow(
      branches=['release-[0-9].[0-9].x', 'k[0-9]*'],
      getDockerCredsFromVault=true,
      imagePrefix='grafana',
      releaseRepo='grafana/loki',
    ), false, false
  ),
}
