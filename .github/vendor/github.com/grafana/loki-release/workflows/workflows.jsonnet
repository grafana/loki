local lokiRelease = import 'main.jsonnet';
local build = lokiRelease.build;
{
  '.github/workflows/release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      imageJobs={
        loki: build.image('fake-loki', 'cmd/loki'),
      },
      branches=['release-[0-9]+.[0-9]+.x'],
      imagePrefix='trevorwhitney075',
      releaseRepo='grafana/loki-release',
      skipValidation=false,
      versioningStrategy='always-bump-patch',
    ), false, false
  ),
  '.github/workflows/release.yml': std.manifestYamlDoc(
    lokiRelease.releaseWorkflow(
      branches=['release-[0-9]+.[0-9]+.x'],
      dockerUsername='trevorwhitney075',
      getDockerCredsFromVault=false,
      imagePrefix='trevorwhitney075',
      releaseRepo='grafana/loki-release',
    ), false, false
  ),
  '.github/workflows/check.yml': std.manifestYamlDoc(
    lokiRelease.check(
      buildImage='grafana/loki-build-image:0.33.0'
    )
  ),
}
