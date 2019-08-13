local pipeline(name) = {
  kind: 'pipeline',
  name: name,
  steps: [],
};

local docker(arch, app) = {
  name: '%s-image' % app,
  image: 'plugins/docker',
  settings: {
    repo: 'shorez/%s' % app,
    dockerfile: 'cmd/%s/Dockerfile' % app,
    // auto_tag: true,
    // auto_tag_suffix: arch,
    username: { from_secret: 'docker_username' },
    password: { from_secret: 'docker_password' },
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
        auto_tag: true,
        ignore_missing: true,
        username: { from_secret: 'docker_username' },
        password: { from_secret: 'docker_password' },
      },
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

  // manifest(['promtail', 'loki', 'loki-canary']),
];

{
  drone: std.manifestYamlStream(drone),
}
