local apps = ['loki', 'loki-canary', 'promtail'];
local archs = ['amd64', 'arm64', 'arm'];

local condition(verb) = {
  tagMaster: {
    ref: {
      [verb]:
        [
          'refs/heads/master',
          'refs/tags/v*',
        ],
    },
  },
};

local pipeline(name) = {
  kind: 'pipeline',
  name: name,
  steps: [],
};

local docker(arch, app) = {
  name: '%s-image' % if $.settings.dry_run then 'build-' + app else 'publish-' + app,
  image: 'plugins/docker',
  settings: {
    repo: 'grafanasaur/%s' % app,
    dockerfile: 'cmd/%s/Dockerfile' % app,
    username: { from_secret: 'saur_username' },
    password: { from_secret: 'saur_password' },
    dry_run: false,
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
    // dry run for everything that is not tag or master
    docker(arch, app) {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMaster,
      settings+: { dry_run: true },
    }
    for app in apps
  ] + [
    // publish for tag or master
    docker(arch, app) {
      depends_on: ['image-tag'],
      when: condition('include').tagMaster,
    }
    for app in apps
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
    'docker-%s' % arch
    for arch in archs
  ],
};

local drone = [
  multiarch_image(arch)
  for arch in archs
] + [
  manifest(['promtail', 'loki', 'loki-canary']) {
    trigger: condition('include').tagMaster,
  },
];

{
  drone: std.manifestYamlStream(drone),
}
