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
      base: "/go/src",
      path: "github.com/grafana/loki"
    },
    steps: [
      make('test', container=false) { depends_on: ['clone'] },
      make('lint', container=false) { depends_on: ['clone'] },
      make('check-generated-files', container=false) { depends_on: ['clone'] },
    ],
  },
] + [
  multiarch_image(arch)
  for arch in archs
] + [
  manifest(['promtail', 'loki', 'loki-canary']) {
    trigger: condition('include').tagMaster,
  },
] + [
  pipeline("deploy") {
    trigger: condition('include').tagMaster,
    depends_on: ["manifest"],
    steps: [
      {
        name: "trigger",
        image: 'grafana/loki-build-image:%s' % build_image_version,
        environment: {
          CIRCLE_TOKEN: {from_secret: "circle_token"}
        },
        commands: [
          'curl -s --header "Content-Type: application/json" --data "{\\"build_parameters\\": {\\"CIRCLE_JOB\\": \\"deploy\\", \\"IMAGE_NAMES\\": \\"$(make print-images)\\"}}" --request POST https://circleci.com/api/v1.1/project/github/raintank/deployment_tools/tree/master?circle-token=$CIRCLE_TOKEN'
        ]
      }
    ],
  }
];

{
  drone: std.manifestYamlStream(drone),
}
