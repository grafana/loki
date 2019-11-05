local apps = ['loki', 'loki-canary', 'promtail'];
local archs = ['amd64', 'arm64', 'arm'];

local build_image_version = std.extVar('__build-image-version');

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

local run(name, commands) = {
  name: name,
  image: 'grafana/loki-build-image:%s' % build_image_version,
  commands: commands,
};

local make(target, container=true) = run(target, [
  'make ' + (if !container then 'BUILD_IN_CONTAINER=false ' else '') + target,
]);

local docker(arch, app) = {
  name: '%s-image' % if $.settings.dry_run then 'build-' + app else 'publish-' + app,
  image: 'plugins/docker',
  settings: {
    repo: 'grafana/%s' % app,
    dockerfile: 'cmd/%s/Dockerfile' % app,
    username: { from_secret: 'docker_username' },
    password: { from_secret: 'docker_password' },
    dry_run: false,
  },
};

local arch_image(arch, tags='') = {
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
    ] + if tags != '' then ['echo ",%s" >> .tags' % tags] else [],
  }],
};

local fluentbit() = pipeline('fluent-bit-amd64') + arch_image('amd64', 'latest,master') {
  steps+: [
    // dry run for everything that is not tag or master
    docker('amd64', 'fluent-bit') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMaster,
      settings+: {
        dry_run: true,
        repo: 'grafana/fluent-bit-plugin-loki',
      },
    },
  ] + [
    // publish for tag or master
    docker('amd64', 'fluent-bit') {
      depends_on: ['image-tag'],
      when: condition('include').tagMaster,
      settings+: {
        repo: 'grafana/fluent-bit-plugin-loki',
      },
    },
  ],
  depends_on: ['check'],
};

local multiarch_image(arch) = pipeline('docker-' + arch) + arch_image(arch) {
  steps+: [
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
  depends_on: ['check'],
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
  depends_on: [
    'docker-%s' % arch
    for arch in archs
  ],
};

local drone = [
  pipeline('check') {
    workspace: {
      base: '/src',
      path: 'loki',
    },
    steps: [
      make('test', container=false) { depends_on: ['clone'] },
      make('lint', container=false) { depends_on: ['clone'] },
      make('check-generated-files', container=false) { depends_on: ['clone'] },
      make('check-mod', container=false) { depends_on: ['clone', 'test', 'lint'] },
    ],
  },
] + [
  multiarch_image(arch)
  for arch in archs
] + [
  fluentbit(),
] + [
  manifest(['promtail', 'loki', 'loki-canary']) {
    trigger: condition('include').tagMaster,
  },
] + [
  pipeline('deploy') {
    trigger: condition('include').tagMaster,
    depends_on: ['manifest'],
    steps: [
      {
        name: 'trigger',
        image: 'grafana/loki-build-image:%s' % build_image_version,
        environment: {
          CIRCLE_TOKEN: { from_secret: 'circle_token' },
        },
        commands: [
          './tools/deploy.sh',
        ],
      },
    ],
  },
];

{
  drone: std.manifestYamlStream(drone),
}
