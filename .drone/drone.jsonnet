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

local docker_build(arch, app) = {
  name: 'build-%s-image' % app,
  image: 'docker',
  commands: [
    'docker build -f cmd/%s/Dockerfile -t grafana/%s:$(./tools/image-tag)-%s' % [app, app, arch],
  ],
  volumes: [
    "/var/run/docker.sock:/var/run/docker.sock"
  ],
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

local helm_test() = {
  name: 'helm-test',
  when: condition('exclude').tagMaster,
  environment:{
    CT_VERSION: '2.3.3',
  },
  image: 'ubuntu:16.04',
  depends_on: ['docker-amd64'],
  command:[
    'curl -sfL https://get.k3s.io | sh -',
    'sudo chmod 755 /etc/rancher/k3s/k3s.yaml',
    'mkdir -p ~/.kube',
    'cp /etc/rancher/k3s/k3s.yaml ~/.kube/config',
    'curl -L https://git.io/get_helm.sh | bash',
    'kubectl apply -f tools/helm.yaml',
    'helm init --service-account helm --wait',
    'pip install yamale yamllint',
    'curl -Lo ct.tgz https://github.com/helm/chart-testing/releases/download/v${CT_VERSION}/chart-testing_${CT_VERSION}_linux_amd64.tar.gz',
    'sudo tar -C /usr/local/bin -xvf ct.tgz',
    'sudo mv /usr/local/bin/etc /etc/ct/',
    "ct lint "+
      "--helm-extra-args='"+
      "--set loki.image.tag=$(./tools/image-tag)-amd64 --set promtail.image.tag=$(./tools/image-tag)-amd64'"+
      "--chart-dirs=production/helm --check-version-increment=false --validate-maintainers=false",
  ],
};

local fluentbit() = pipeline('fluent-bit-amd64') + arch_image('amd64', 'latest,master') {
  steps+: [
    // dry run for everything that is not tag or master
    docker('amd64', 'fluent-bit') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMaster,
      settings+: {
        dry_run: true,
        repo: 'grafanasaur/fluent-bit-plugin-loki',
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
    // for everything that is not tag or master: build and tag only.
    docker_build(arch, app) {
      when: condition('exclude').tagMaster,
    }
    for app in apps
  ] + [helm_test()] + [
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
