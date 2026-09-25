local lokiRelease = import 'main.jsonnet';
local build = lokiRelease.build;


local buildImage = std.extVar('BUILD_IMAGE');
local dockerPluginDir = 'clients/cmd/docker-driver';

{
  '.github/workflows/release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      imageJobs={
        loki: build.image('fake-loki', 'cmd/loki'),
        'loki-docker-driver': build.dockerPlugin('loki-docker-driver', dockerPluginDir, buildImage=buildImage),
      },
      buildImage=buildImage,
      buildArtifactsBucket='loki-build-artifacts',
      branches=['release-[0-9]+.[0-9]+.x'],
      imagePrefix='us-docker.pkg.dev/grafanalabs-dev/docker-loki-dev',
      releaseLibRef='main',
      releaseRepo='grafana/loki-release',
      distRunsOn='ubuntu-26.04',
      distOptionalTargets=['dist/loki-linux-riscv64'],
      skipValidation=false,
      versioningStrategy='always-bump-patch',
    ) + {
      name: 'Create Release PR',
    }, false, false
  ),
  '.github/workflows/test-release-pr.yml': std.manifestYamlDoc(
    lokiRelease.releasePRWorkflow(
      imageJobs={
        loki: build.image('fake-loki', 'cmd/loki'),
        'loki-docker-driver': build.dockerPlugin('loki-docker-driver', dockerPluginDir, buildImage=buildImage),
      },
      buildImage=buildImage,
      buildArtifactsBucket='loki-build-artifacts',
      branches=['release-[0-9]+.[0-9]+.x'],
      dryRun=true,
      imagePrefix='us-docker.pkg.dev/grafanalabs-dev/docker-loki-dev',
      releaseLibRef='main',
      releaseRepo='grafana/loki-release',
      skipValidation=false,
      versioningStrategy='always-bump-patch',
    ) + {
      name: 'Test Create Release PR Action',
      on+: {
        pull_request: {},
      },
      env+: {
        IMAGE_PREFIX: 'us-docker.pkg.dev/grafanalabs-dev/docker-loki-dev/ci/${{ github.run_id }}',
        PLUGIN_IMAGE_PREFIX: 'us-docker.pkg.dev/grafanalabs-dev/docker-loki-dev/ci/${{ github.run_id }}',
      },
      jobs+: {
        publishImages: lokiRelease.release.publishImages(
          needs=['loki', 'loki-docker-driver'],
          sha='${{ github.sha }}',
          isLatest='false',
        ),
        publishDockerPlugins: lokiRelease.release.publishDockerPlugins(
          dockerPluginDir,
          needs=['loki', 'loki-docker-driver'],
          sha='${{ github.sha }}',
          isLatest='false',
        ),
      },
    }, false, false
  ),
  '.github/workflows/release.yml': std.manifestYamlDoc(
    lokiRelease.releaseWorkflow(
      branches=['release-[0-9]+.[0-9]+.x'],
      buildArtifactsBucket='loki-build-artifacts',
      imagePrefix='us-docker.pkg.dev/grafanalabs-dev/docker-loki-dev',
      pluginBuildDir=dockerPluginDir,
      releaseLibRef='main',
      releaseRepo='grafana/loki-release',
    ) + {
      name: 'Create Release',
      on+: {
        pull_request: {},
      },
    }, false, false
  ),
  '.github/workflows/check.yml': std.manifestYamlDoc(
    lokiRelease.check
  ),
  '.github/workflows/gel-check.yml': std.manifestYamlDoc(
    lokiRelease.checkGel
  ),
}
