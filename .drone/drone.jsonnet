local apps = ['loki', 'loki-canary', 'logcli'];
local archs = ['amd64', 'arm64', 'arm'];

local build_image_version = std.extVar('__build-image-version');

local condition(verb) = {
  tagMain: {
    ref: {
      [verb]:
        [
          'refs/heads/main',
          'refs/heads/k??',
          'refs/tags/v*',
        ],
    },
  },
  path(path): {
    paths: {
      [verb]: [path],
    },
  },
};

local pipeline(name) = {
  kind: 'pipeline',
  name: name,
  steps: [],
  trigger: { event: ['push', 'pull_request', 'tag'] },
};

local secret(name, vault_path, vault_key) = {
  kind: 'secret',
  name: name,
  get: {
    path: vault_path,
    name: vault_key,
  },
};
local docker_username_secret = secret('docker_username', 'infra/data/ci/docker_hub', 'username');
local docker_password_secret = secret('docker_password', 'infra/data/ci/docker_hub', 'password');
local ecr_key = secret('ecr_key', 'infra/data/ci/loki/aws-credentials', 'access_key_id');
local ecr_secret_key = secret('ecr_secret_key', 'infra/data/ci/loki/aws-credentials', 'secret_access_key');
local pull_secret = secret('dockerconfigjson', 'secret/data/common/gcr', '.dockerconfigjson');
local github_secret = secret('github_token', 'infra/data/ci/github/grafanabot', 'pat');

// Injected in a secret because this is a public repository and having the config here would leak our environment names
local deploy_configuration = secret('deploy_config', 'secret/data/common/loki_ci_autodeploy', 'config.json');

local run(name, commands, env={}) = {
  name: name,
  image: 'grafana/loki-build-image:%s' % build_image_version,
  commands: commands,
  environment: env,
};

local make(target, container=true, args=[]) = run(target, [
  std.join(' ', [
    'make',
    'BUILD_IN_CONTAINER=' + container,
    target,
  ] + args),
]);

local docker(arch, app) = {
  name: '%s-image' % if $.settings.dry_run then 'build-' + app else 'publish-' + app,
  image: 'plugins/docker',
  settings: {
    repo: 'grafana/%s' % app,
    dockerfile: 'cmd/%s/Dockerfile' % app,
    username: { from_secret: docker_username_secret.name },
    password: { from_secret: docker_password_secret.name },
    dry_run: false,
  },
};

local clients_docker(arch, app) = {
  name: '%s-image' % if $.settings.dry_run then 'build-' + app else 'publish-' + app,
  image: 'plugins/docker',
  settings: {
    repo: 'grafana/%s' % app,
    dockerfile: 'clients/cmd/%s/Dockerfile' % app,
    username: { from_secret: docker_username_secret.name },
    password: { from_secret: docker_password_secret.name },
    dry_run: false,
  },
};

local lambda_promtail_ecr(app) = {
  name: '%s-image' % if $.settings.dry_run then 'build-' + app else 'publish-' + app,
  image: 'cstyan/ecr',
  privileged: true,
  settings: {
    repo: 'public.ecr.aws/grafana/lambda-promtail',
    registry: 'public.ecr.aws/grafana',
    dockerfile: 'tools/%s/Dockerfile' % app,
    access_key: { from_secret: ecr_key.name },
    secret_key: { from_secret: ecr_secret_key.name },
    dry_run: false,
    region: 'us-east-1',
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

local promtail_win() = pipeline('promtail-windows') {
  platform: {
    os: 'windows',
    arch: 'amd64',
    version: '1809',
  },
  steps: [
    {
      name: 'identify-runner',
      image: 'golang:windowsservercore-1809',
      commands: [
        'Write-Output $env:DRONE_RUNNER_NAME',
      ],
    },
    {
      name: 'test',
      image: 'golang:windowsservercore-1809',
      commands: [
        'go test .\\clients\\pkg\\promtail\\targets\\windows\\... -v',
      ],
    },
  ],
};

local querytee() = pipeline('querytee-amd64') + arch_image('amd64', 'main') {
  steps+: [
    // dry run for everything that is not tag or main
    docker('amd64', 'querytee') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMain,
      settings+: {
        dry_run: true,
        repo: 'grafana/loki-query-tee',
      },
    },
  ] + [
    // publish for tag or main
    docker('amd64', 'querytee') {
      depends_on: ['image-tag'],
      when: condition('include').tagMain,
      settings+: {
        repo: 'grafana/loki-query-tee',
      },
    },
  ],
  depends_on: ['check'],
};

local fluentbit() = pipeline('fluent-bit-amd64') + arch_image('amd64', 'main') {
  steps+: [
    // dry run for everything that is not tag or main
    clients_docker('amd64', 'fluent-bit') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMain,
      settings+: {
        dry_run: true,
        repo: 'grafana/fluent-bit-plugin-loki',
      },
    },
  ] + [
    // publish for tag or main
    clients_docker('amd64', 'fluent-bit') {
      depends_on: ['image-tag'],
      when: condition('include').tagMain,
      settings+: {
        repo: 'grafana/fluent-bit-plugin-loki',
      },
    },
  ],
  depends_on: ['check'],
};

local fluentd() = pipeline('fluentd-amd64') + arch_image('amd64', 'main') {
  steps+: [
    // dry run for everything that is not tag or main
    clients_docker('amd64', 'fluentd') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMain,
      settings+: {
        dry_run: true,
        repo: 'grafana/fluent-plugin-loki',
      },
    },
  ] + [
    // publish for tag or main
    clients_docker('amd64', 'fluentd') {
      depends_on: ['image-tag'],
      when: condition('include').tagMain,
      settings+: {
        repo: 'grafana/fluent-plugin-loki',
      },
    },
  ],
  depends_on: ['check'],
};

local logstash() = pipeline('logstash-amd64') + arch_image('amd64', 'main') {
  steps+: [
    // dry run for everything that is not tag or main
    clients_docker('amd64', 'logstash') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMain,
      settings+: {
        dry_run: true,
        repo: 'grafana/logstash-output-loki',
      },
    },
  ] + [
    // publish for tag or main
    clients_docker('amd64', 'logstash') {
      depends_on: ['image-tag'],
      when: condition('include').tagMain,
      settings+: {
        repo: 'grafana/logstash-output-loki',
      },
    },
  ],
  depends_on: ['check'],
};

local promtail(arch) = pipeline('promtail-' + arch) + arch_image(arch) {
  steps+: [
    // dry run for everything that is not tag or main
    clients_docker(arch, 'promtail') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMain,
      settings+: {
        dry_run: true,
      },
    },
  ] + [
    // publish for tag or main
    clients_docker(arch, 'promtail') {
      depends_on: ['image-tag'],
      when: condition('include').tagMain,
      settings+: {},
    },
  ],
  depends_on: ['check'],
};

local lambda_promtail(tags='') = pipeline('lambda-promtail') {
  steps+: [
    {
      name: 'image-tag',
      image: 'alpine',
      commands: [
        'apk add --no-cache bash git',
        'git fetch origin --tags',
        'echo $(./tools/image-tag)-amd64 > .tags',
      ] + if tags != '' then ['echo ",%s" >> .tags' % tags] else [],
    },
    lambda_promtail_ecr('lambda-promtail') {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMain,
      settings+: {
        dry_run: true,
      },
    },
  ] + [
    // publish for tag or main
    lambda_promtail_ecr('lambda-promtail') {
      depends_on: ['image-tag'],
      when: condition('include').tagMain,
      settings+: {},
    },
  ],
  depends_on: ['check'],
};

local multiarch_image(arch) = pipeline('docker-' + arch) + arch_image(arch) {
  steps+: [
    // dry run for everything that is not tag or main
    docker(arch, app) {
      depends_on: ['image-tag'],
      when: condition('exclude').tagMain,
      settings+: {
        dry_run: true,
      },
    }
    for app in apps
  ] + [
    // publish for tag or main
    docker(arch, app) {
      depends_on: ['image-tag'],
      when: condition('include').tagMain,
      settings+: {},
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
        username: { from_secret: docker_username_secret.name },
        password: { from_secret: docker_password_secret.name },
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
  ] + [
    'promtail-%s' % arch
    for arch in archs
  ],
};

[
  pipeline('loki-build-image') {
    workspace: {
      base: '/src',
      path: 'loki',
    },
    steps: [
      {
        name: 'push-image',
        image: 'plugins/docker',
        when: condition('include').tagMain + condition('include').path('loki-build-image/**'),
        settings: {
          repo: 'grafana/loki-build-image',
          context: 'loki-build-image',
          dockerfile: 'loki-build-image/Dockerfile',
          username: { from_secret: docker_username_secret.name },
          password: { from_secret: docker_password_secret.name },
          tags: ['0.20.4'],
          dry_run: false,
        },
      },
    ],
  },
  pipeline('check') {
    workspace: {
      base: '/src',
      path: 'loki',
    },
    steps: [
      make('check-drone-drift', container=false) { depends_on: ['clone'] },
      make('check-generated-files', container=false) { depends_on: ['clone'] },
      run('clone-main', commands=['cd ..', 'git clone $CI_REPO_REMOTE loki-main', 'cd -']) { depends_on: ['clone'] },
      make('test', container=false) { depends_on: ['clone', 'clone-main'] },
      run('test-main', commands=['cd ../loki-main', 'BUILD_IN_CONTAINER=false make test']) { depends_on: ['clone-main'] },
      make('compare-coverage', container=false, args=[
        'old=../loki-main/test_results.txt',
        'new=test_results.txt',
        'packages=ingester,distributor,querier,querier/queryrange,iter,storage,chunkenc,logql,loki',
        '> diff.txt',
      ]) { depends_on: ['test', 'test-main'] },
      run('report-coverage', commands=[
        "pull=$(echo $CI_COMMIT_REF | awk -F '/' '{print $3}')",
        "body=$(jq -Rs '{body: . }' diff.txt)",
        'curl -X POST -u $USER:$TOKEN -H "Accept: application/vnd.github.v3+json" https://api.github.com/repos/grafana/loki/issues/$pull/comments -d "$body" > /dev/null',
      ], env={
        USER: 'grafanabot',
        TOKEN: { from_secret: github_secret.name },
      }) { depends_on: ['compare-coverage'] },
      make('lint', container=false) { depends_on: ['clone', 'check-generated-files'] },
      make('check-mod', container=false) { depends_on: ['clone', 'test', 'lint'] },
      {
        name: 'shellcheck',
        image: 'koalaman/shellcheck-alpine:stable',
        commands: ['apk add make bash && make lint-scripts'],
      },
      make('loki', container=false) { depends_on: ['clone', 'check-generated-files'] },
      make('validate-example-configs', container=false) { depends_on: ['loki'] },
      make('check-example-config-doc', container=false) { depends_on: ['clone'] },
    ],
  },
  pipeline('mixins') {
    workspace: {
      base: '/src',
      path: 'loki',
    },
    steps: [
      make('lint-jsonnet', container=false) {
        // Docker image defined at https://github.com/grafana/jsonnet-libs/tree/master/build
        image: 'grafana/jsonnet-build:c8b75df',
        depends_on: ['clone'],
      },
    ],
  },
] + [
  multiarch_image(arch)
  for arch in archs
] + [
  promtail(arch) + (
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
          if std.objectHas(step, 'settings') && step.settings.dockerfile == 'clients/cmd/promtail/Dockerfile'
          then {
            settings+: {
              dockerfile: 'clients/cmd/promtail/Dockerfile.arm32',
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
  querytee(),
] + [
  manifest(['promtail', 'loki', 'loki-canary']) {
    trigger: condition('include').tagMain {
      event: ['push', 'tag'],
    },
  },
] + [
  pipeline('deploy') {
    trigger: condition('include').tagMain {
      event: ['push', 'tag'],
    },
    depends_on: ['manifest'],
    image_pull_secrets: [pull_secret.name],
    steps: [
      {
        name: 'image-tag',
        image: 'alpine',
        commands: [
          'apk add --no-cache bash git',
          'git fetch origin --tags',
          'echo $(./tools/image-tag) > .tag',
        ],
        depends_on: ['clone'],
      },
      {
        name: 'trigger',
        image: 'us.gcr.io/kubernetes-dev/drone/plugins/deploy-image',
        settings: {
          github_token: { from_secret: github_secret.name },
          images_json: { from_secret: deploy_configuration.name },
          docker_tag_file: '.tag',
        },
        depends_on: ['clone', 'image-tag'],
      },
    ],
  },
] + [promtail_win()]
+ [lambda_promtail('main')]
+ [github_secret, pull_secret, docker_username_secret, docker_password_secret, ecr_key, ecr_secret_key, deploy_configuration]
