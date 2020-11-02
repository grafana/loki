local apps = ['loki', 'loki-canary', 'promtail','logcli'];
local archs = ['amd64', 'arm64', 'arm'];

local build_image_version = std.extVar('__build-image-version');

local condition(verb) = {
  tagMaster: {
    ref: {
      [verb]:
        [
          'refs/heads/master',
          'refs/heads/k??',
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

local fluentd() = pipeline('fluentd-amd64') + arch_image('amd64', 'latest,master') {
  steps+: [
    // dry run for everything that is not tag or master
    docker('amd64', 'fluentd') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMaster,
      settings+: {
        dry_run: true,
        repo: 'grafana/fluent-plugin-loki',
      },
    },
  ] + [
    // publish for tag or master
    docker('amd64', 'fluentd') {
      depends_on: ['image-tag'],
      when: condition('include').tagMaster,
      settings+: {
        repo: 'grafana/fluent-plugin-loki',
      },
    },
  ],
  depends_on: ['check'],
};

local logstash() = pipeline('logstash-amd64') + arch_image('amd64', 'latest,master') {
  steps+: [
    // dry run for everything that is not tag or master
    docker('amd64', 'logstash') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMaster,
      settings+: {
        dry_run: true,
        repo: 'grafana/logstash-output-loki',
      },
    },
  ] + [
    // publish for tag or master
    docker('amd64', 'logstash') {
      depends_on: ['image-tag'],
      when: condition('include').tagMaster,
      settings+: {
        repo: 'grafana/logstash-output-loki',
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
      settings+: {
        dry_run: true,
        build_args: ['TOUCH_PROTOS=1'],
      },
    }
    for app in apps
  ] + [
    // publish for tag or master
    docker(arch, app) {
      depends_on: ['image-tag'],
      when: condition('include').tagMaster,
      settings+: {
        build_args: ['TOUCH_PROTOS=1'],
      },
    }
    for app in apps
  ],
  depends_on: ['check'],
};

local manifest(apps) = pipeline('manifest') {
  steps: std.foldl(
    function(acc, app) acc + [{
      name: 'manifest-' + app,
      image: 'plugins/manifest',
      settings: {
        // the target parameter is abused for the app's name,
        // as it is unused in spec mode. See docker-manifest.tmpl
        target: app,
        spec: '.drone/docker-manifest.tmpl',
        ignore_missing: false,
        username: { from_secret: 'docker_username' },
        password: { from_secret: 'docker_password' },
      },
      depends_on: ['clone'] + (
        // Depend on the previous app, if any.
        if std.length(acc) > 0
        then [acc[std.length(acc) - 1].name]
        else []
      ),
    }],
    apps,
    [],
  ),
  depends_on: [
    'docker-%s' % arch
    for arch in archs
  ],
};

[
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
  multiarch_image(arch) + (
    // When we're building Promtail for ARM, we want to use Dockerfile.arm32 to fix
    // a problem with the published Drone image. See Dockerfile.arm32 for more
    // information.
    //
    // This is really really hacky and a better more permanent solution will be to use
    // buildkit.
    if arch == 'arm'
    then {
      steps: [
        step + (
          if std.objectHas(step, 'settings') && step.settings.dockerfile == 'cmd/promtail/Dockerfile'
          then {
            settings+: {
              dockerfile: 'cmd/promtail/Dockerfile.arm32',
            },
          }
          else {}
        )
        for step in super.steps
      ],
    }
    else {}
  )
  for arch in archs
] + [
  fluentbit(),
  fluentd(),
  logstash(),
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
        depends_on: ['clone'],
      },
    ],
  },
] + [
  pipeline('prune-ci-tags') {
    trigger: condition('include').tagMaster,
    depends_on: ['manifest'],
    steps: [
      {
        name: 'trigger',
        image: 'grafana/loki-build-image:%s' % build_image_version,
        environment: {
          DOCKER_USERNAME: { from_secret: 'docker_username' },
          DOCKER_PASSWORD: { from_secret: 'docker_password' },
        },
        commands: [
          'go run ./tools/delete_tags.go -max-age=2160h -repo grafana/loki -delete',
          'go run ./tools/delete_tags.go -max-age=2160h -repo grafana/promtail -delete',
          'go run ./tools/delete_tags.go -max-age=2160h -repo grafana/loki-canary -delete',
        ],
      },
    ],
  },
]
