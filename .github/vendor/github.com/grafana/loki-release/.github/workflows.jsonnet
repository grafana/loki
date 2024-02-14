local lokiRelease = import '../main.jsonnet';
local build = lokiRelease.build;
{
  'release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      imageJobs={
        loki: build.image('fake-loki', 'cmd/loki'),
      },
      branches=['release-[0-9].[0-9].x'],
      imagePrefix='grafana',
      releaseRepo='grafana/loki-release',
      skipValidation=false,
      versioningStrategy='always-bump-patch',
    ), false, false
  ),
  'release.yml': std.manifestYamlDoc(
    lokiRelease.releaseWorkflow(
      branches=['release-[0-9].[0-9].x'],
      getDockerCredsFromVault=true,
      imagePrefix='grafana',
      releaseRepo='grafana/loki-release',
    ), false, false
  ),
}
