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
      releaseLibRef='release-1.12.x',
      releaseRepo='grafana/loki-release',
      skipValidation=false,
      versioningStrategy='always-bump-patch',
    ), false, false
  ),
  '.github/workflows/release.yml': std.manifestYamlDoc(
    lokiRelease.releaseWorkflow(
      branches=['release-[0-9]+.[0-9]+.x'],
      getDockerCredsFromVault=true,
      imagePrefix='trevorwhitney075',
      releaseLibRef='release-1.12.x',
      releaseRepo='grafana/loki-release',
      useGitHubAppToken=false,
    ), false, false
  ),
  '.github/workflows/check.yml': std.manifestYamlDoc(
    lokiRelease.check
  ),
  '.github/workflows/gel-check.yml': std.manifestYamlDoc(
    lokiRelease.checkGel
  ),
}
