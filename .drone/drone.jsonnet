local pipeline(name) = {
  kind: 'pipeline',
  name: name,
  steps: [],
};

local docker(arch, app) = {
  name: '%s-image' % app,
  image: 'plugins/docker',
  settings: {
    repo: 'grafana/%s' % app,
    dockerfile: 'cmd/%s/Dockerfile' % app,
    username: { from_secret: 'docker_username' },
    password: { from_secret: 'docker_password' },
    //TODO: disable once considered stable
    dry_run: true,
  },
};

local multiarch_image(arch) = pipeline('docker-' + arch) {
  platform: {
    os: 'linux',
    arch: arch,
  },
  steps: [{
    name: 'image-tag',
    image: 'alpine',
    commands: [
      'apk add --no-cache bash git',
      'git fetch origin --tags',
      'echo $(./tools/image-tag)-%s > .tags' % arch,
    ],
  }] + [
    docker(arch, app) { depends_on: ['image-tag'] }
    for app in ['promtail', 'loki', 'loki-canary']
  ],
};

local manifest(apps) = pipeline('manifest') {
  steps: [
    {
      name: 'manifest-' + app,
      image: 'plugins/manifest',
      settings: {
        // the target parameter is abused for the app's name,
        // as it is unused in spec mode. See docker-manifest.tmpl
        target: app,
        spec: '.drone/docker-manifest.tmpl',
        ignore_missing: true,
        username: { from_secret: 'docker_username' },
        password: { from_secret: 'docker_password' },
      },
      depends_on: ['clone'],
    }
    for app in apps
  ],
} + {
  depends_on: [
    'docker-amd64',
    'docker-arm',
    'docker-arm64',
  ],
};

local drone = [
  multiarch_image('amd64'),
  multiarch_image('arm64'),
  multiarch_image('arm'),

  // tie it all up as a fast manifest
  // TODO: enable once considered stable
  // manifest(['promtail', 'loki', 'loki-canary']),
];

{
  drone: std.manifestYamlStream(drone),
}
