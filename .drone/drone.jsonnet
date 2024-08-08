local apps = ['loki', 'loki-canary', 'loki-canary-boringcrypto', 'logcli'];
local archs = ['amd64', 'arm64', 'arm'];

local build_image_version = std.extVar('__build-image-version');

local drone_updater_plugin_image = 'us.gcr.io/kubernetes-dev/drone/plugins/updater@sha256:cbcb09c74f96a34c528f52bf9b4815a036b11fed65f685be216e0c8b8e84285b';

local onPRs = {
  event: ['pull_request'],
};

local onTagOrMain = {
  event: ['push', 'tag'],
};

local onTag = {
  event: ['tag'],
};

local onPath(path) = {
  paths+: [path],
};

local pipeline(name) = {
  kind: 'pipeline',
  name: name,
  steps: [],
  trigger: {
    // Only trigger pipelines for PRs, tags (v*), or pushes to "main". Excluding runs on grafana/loki (non fork) branches
    ref: ['refs/heads/main', 'refs/heads/k???', 'refs/tags/v*', 'refs/pull/*/head'],
  },
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
local gpg_passphrase = secret('gpg_passphrase', 'infra/data/ci/packages-publish/gpg', 'passphrase');
local gpg_private_key = secret('gpg_private_key', 'infra/data/ci/packages-publish/gpg', 'private-key');

// Injected in a secret because this is a public repository and having the config here would leak our environment names
local updater_config_template = secret('updater_config_template', 'secret/data/common/loki_ci_autodeploy', 'updater-config-template.json');
local helm_chart_auto_update_config_template = secret('helm-chart-update-config-template', 'secret/data/common/loki-helm-chart-auto-update', 'on-loki-release-config.json');


local run(name, commands, env={}, image='grafana/loki-build-image:%s' % build_image_version) = {
  name: name,
  image: image,
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

// The only indication we have that we're running in a fork is the presence of a secret.
// If a secret is blank, it means we're running in a fork.
local skipMissingSecretPipelineStep(secretName) = run(
  'skip pipeline if missing secret',
  [
    'if [ "$${#TEST_SECRET}" -eq 0 ]; then',
    '  echo "Missing a secret to run this pipeline. This branch needs to be re-pushed as a branch in main grafana/loki repository in order to run." && exit 78',
    'fi',
  ],
  image='alpine',
  env={
    TEST_SECRET: { from_secret: secretName },
  },
);

local docker(arch, app) = {
  name: '%s-image' % if $.settings.dry_run then 'build-' + app else 'publish-' + app,
  image: if arch == 'arm' then 'plugins/docker:linux-arm' else 'plugins/docker',
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
  image: if arch == 'arm' then 'plugins/docker:linux-arm' else 'plugins/docker',
  settings: {
    repo: 'grafana/%s' % app,
    dockerfile: 'clients/cmd/%s/Dockerfile' % app,
    username: { from_secret: docker_username_secret.name },
    password: { from_secret: docker_password_secret.name },
    dry_run: false,
  },
};

local docker_operator(arch, operator) = {
  name: '%s-image' % if $.settings.dry_run then 'build-' + operator else 'publish-' + operator,
  image: if arch == 'arm' then 'plugins/docker:linux-arm' else 'plugins/docker',
  settings: {
    repo: 'grafana/%s' % operator,
    context: 'operator',
    dockerfile: 'operator/Dockerfile',
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

local querytee() = pipeline('querytee-amd64') + arch_image('amd64', 'main') {
  steps+: [
    // publish for tag or main
    docker('amd64', 'querytee') {
      depends_on: ['image-tag'],
      when: onTagOrMain,
      settings+: {
        repo: 'grafana/loki-query-tee',
      },
    },
  ],
};

local fluentbit(arch) = pipeline('fluent-bit-' + arch) + arch_image(arch) {
  steps+: [
    // publish for tag or main
    clients_docker(arch, 'fluent-bit') {
      depends_on: ['image-tag'],
      when: onTagOrMain,
      settings+: {
        repo: 'grafana/fluent-bit-plugin-loki',
      },
    },
  ],
};

local fluentd() = pipeline('fluentd-amd64') + arch_image('amd64', 'main') {
  steps+: [
    // publish for tag or main
    clients_docker('amd64', 'fluentd') {
      depends_on: ['image-tag'],
      when: onTagOrMain,
      settings+: {
        repo: 'grafana/fluent-plugin-loki',
      },
    },
  ],
};

local logstash() = pipeline('logstash-amd64') + arch_image('amd64', 'main') {
  steps+: [
    // publish for tag or main
    clients_docker('amd64', 'logstash') {
      depends_on: ['image-tag'],
      when: onTagOrMain,
      settings+: {
        repo: 'grafana/logstash-output-loki',
      },
    },
  ],
};

local promtail(arch) = pipeline('promtail-' + arch) + arch_image(arch) {
  steps+: [
    // publish for tag or main
    clients_docker(arch, 'promtail') {
      depends_on: ['image-tag'],
      when: onTagOrMain,
      settings+: {},
    },
  ],
};

local lambda_promtail(arch) = pipeline('lambda-promtail-' + arch) + arch_image(arch) {
  local skipStep = skipMissingSecretPipelineStep(ecr_key.name),  // Needs ECR secrets to run

  steps+: [
    skipStep,
    // publish for tag or main
    lambda_promtail_ecr('lambda-promtail') {
      depends_on: ['image-tag'],
      when: onTagOrMain,
      settings+: {},
    },
  ],
};

local lokioperator(arch) = pipeline('lokioperator-' + arch) + arch_image(arch) {
  steps+: [
    // publish for tag or main
    docker_operator(arch, 'loki-operator') {
      depends_on: ['image-tag'],
      when: onTagOrMain {
        ref: ['refs/heads/main', 'refs/tags/operator/v*'],
      },
      settings+: {},
    },
  ],
};

local logql_analyzer() = pipeline('logql-analyzer') + arch_image('amd64') {
  steps+: [
    // publish for tag or main
    docker('amd64', 'logql-analyzer') {
      depends_on: ['image-tag'],
      when: onTagOrMain,
      settings+: {
        repo: 'grafana/logql-analyzer',
      },
    },
  ],
};

local multiarch_image(arch) = pipeline('docker-' + arch) + arch_image(arch) {
  steps+: [
    // publish for tag or main
    docker(arch, app) {
      depends_on: ['image-tag'],
      when: onTagOrMain,
      settings+: {},
    }
    for app in apps
  ],
};

local manifest(apps) = pipeline('manifest') {
  steps: std.foldl(
    function(acc, app) acc + [{
      name: 'manifest-' + app,
      image: 'plugins/manifest:1.4.0',
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
  ] + [
    'fluent-bit-%s' % arch
    for arch in archs
  ],
};

local manifest_operator(app) = pipeline('manifest-operator') {
  steps: [{
    name: 'manifest-' + app,
    image: 'plugins/manifest:1.4.0',
    settings: {
      // the target parameter is abused for the app's name,
      // as it is unused in spec mode. See docker-manifest-operator.tmpl
      target: app,
      spec: '.drone/docker-manifest-operator.tmpl',
      ignore_missing: false,
      username: { from_secret: docker_username_secret.name },
      password: { from_secret: docker_password_secret.name },
    },
    depends_on: ['clone'],
  }],
  depends_on: [
    'lokioperator-%s' % arch
    for arch in archs
  ],
};


local manifest_ecr(apps, archs) = pipeline('manifest-ecr') {
  steps: std.foldl(
    function(acc, app) acc + [{
      name: 'manifest-' + app,
      image: 'plugins/manifest:1.4.0',
      volumes: [{
        name: 'dockerconf',
        path: '/.docker',
      }],
      settings: {
        // the target parameter is abused for the app's name,
        // as it is unused in spec mode. See docker-manifest-ecr.tmpl
        target: app,
        spec: '.drone/docker-manifest-ecr.tmpl',
        ignore_missing: true,
      },
      depends_on: ['clone'] + (
        // Depend on the previous app, if any.
        if std.length(acc) > 0
        then [acc[std.length(acc) - 1].name]
        else []
      ),
    }],
    apps,
    [{
      name: 'ecr-login',
      image: 'docker:dind',
      volumes: [{
        name: 'dockerconf',
        path: '/root/.docker',
      }],
      environment: {
        AWS_ACCESS_KEY_ID: { from_secret: ecr_key.name },
        AWS_SECRET_ACCESS_KEY: { from_secret: ecr_secret_key.name },
      },
      commands: [
        'apk add --no-cache aws-cli',
        'docker login --username AWS --password $(aws ecr-public get-login-password --region us-east-1) public.ecr.aws',
      ],
      depends_on: ['clone'],
    }],
  ),
  volumes: [{
    name: 'dockerconf',
    temp: {},
  }],
  depends_on: [
    'lambda-promtail-%s' % arch
    for arch in archs
  ],
};

local build_image_tag = '0.33.2';
[
  pipeline('loki-build-image-' + arch) {
    workspace: {
      base: '/src',
      path: 'loki',
    },
    platform: {
      os: 'linux',
      arch: arch,
    },
    steps: [
      {
        name: 'push',
        image: 'plugins/docker',
        when: onTagOrMain + onPath('loki-build-image/**'),
        environment: {
          DOCKER_BUILDKIT: 1,
        },
        settings: {
          repo: 'grafana/loki-build-image',
          context: 'loki-build-image',
          dockerfile: 'loki-build-image/Dockerfile',
          username: { from_secret: docker_username_secret.name },
          password: { from_secret: docker_password_secret.name },
          tags: [build_image_tag + '-' + arch],
          dry_run: false,
        },
      },
    ],
  }
  for arch in ['amd64', 'arm64']
] + [
  pipeline('loki-build-image-publish') {
    steps: [
      {
        name: 'manifest',
        image: 'plugins/manifest:1.4.0',
        when: onTagOrMain + onPath('loki-build-image/**'),
        settings: {
          // the target parameter is abused for the app's name, as it is unused in spec mode.
          target: 'loki-build-image:' + build_image_tag,
          spec: '.drone/docker-manifest-build-image.tmpl',
          ignore_missing: false,
          username: { from_secret: docker_username_secret.name },
          password: { from_secret: docker_password_secret.name },
        },
      },
    ],
    depends_on: [
      'loki-build-image-%s' % arch
      for arch in ['amd64', 'arm64']
    ],
  },
  pipeline('helm-test-image') {
    workspace: {
      base: '/src',
      path: 'loki',
    },
    steps: [
      {
        name: 'push-image',
        image: 'plugins/docker',
        when: onTagOrMain + onPath('production/helm/loki/src/helm-test/**'),
        settings: {
          repo: 'grafana/loki-helm-test',
          dockerfile: 'production/helm/loki/src/helm-test/Dockerfile',
          username: { from_secret: docker_username_secret.name },
          password: { from_secret: docker_password_secret.name },
          dry_run: false,
        },
      },
    ],
  },
  pipeline('documentation-checks') {
    workspace: {
      base: '/src',
      path: 'loki',
    },
    steps: [
      make('documentation-helm-reference-check', container=false) {
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
  lokioperator(arch) {
    trigger+: {
      ref: [
        'refs/heads/main',
        'refs/tags/operator/v*',
        'refs/pull/*/head',
      ],
    },
  }
  for arch in archs
] + [
  fluentbit(arch)
  for arch in archs
] + [
  fluentd(),
  logstash(),
  querytee(),
  manifest(['promtail', 'loki', 'loki-canary', 'loki-canary-boringcrypto', 'fluent-bit-plugin-loki']) {
    trigger+: onTagOrMain,
  },
  manifest_operator('loki-operator') {
    trigger+: onTagOrMain {
      ref: [
        'refs/heads/main',
        'refs/tags/operator/v*',
      ],
    },
  },
  pipeline('deploy') {
    local configFileName = 'updater-config.json',
    trigger: onTagOrMain {
      ref: ['refs/heads/main', 'refs/tags/v*'],
    },
    depends_on: ['manifest'],
    image_pull_secrets: [pull_secret.name],
    steps: [
      {
        name: 'prepare-updater-config',
        image: 'alpine',
        environment: {
          MAJOR_MINOR_VERSION_REGEXP: '([0-9]+\\.[0-9]+)',
          RELEASE_TAG_REGEXP: '^([0-9]+\\.[0-9]+\\.[0-9]+)$',
        },
        commands: [
          'apk add --no-cache bash git',
          'git fetch origin --tags',
          'echo $(./tools/image-tag) > .tag',
          'export RELEASE_TAG=$(cat .tag)',
          // if the tag matches the pattern `D.D.D` then RELEASE_NAME="D-D-x", otherwise RELEASE_NAME="next"
          'export RELEASE_NAME=$([[ $RELEASE_TAG =~ $RELEASE_TAG_REGEXP ]] && echo $RELEASE_TAG | grep -oE $MAJOR_MINOR_VERSION_REGEXP | sed "s/\\./-/g" | sed "s/$/-x/" || echo "next")',
          'echo $RELEASE_NAME',
          'echo $PLUGIN_CONFIG_TEMPLATE > %s' % configFileName,
          // replace placeholders with RELEASE_NAME and RELEASE TAG
          'sed -i "s/\\"{{release}}\\"/\\"$RELEASE_NAME\\"/g" %s' % configFileName,
          'sed -i "s/{{version}}/$RELEASE_TAG/g" %s' % configFileName,
        ],
        settings: {
          config_template: { from_secret: updater_config_template.name },
        },
        depends_on: ['clone'],
      },
      {
        name: 'trigger',
        image: drone_updater_plugin_image,
        settings: {
          github_token: { from_secret: github_secret.name },
          config_file: configFileName,
        },
        depends_on: ['prepare-updater-config'],
      },
    ],
  },
  pipeline('update-loki-helm-chart-on-loki-release') {
    local configFileName = 'updater-config.json',
    depends_on: ['manifest'],
    image_pull_secrets: [pull_secret.name],
    trigger: {
      // we need to run it only on Loki tags that starts with `v`.
      ref: ['refs/tags/v*'],
    },
    steps: [
      {
        name: 'check-version-is-latest',
        image: 'alpine',
        when: onTag,
        commands: [
          'apk add --no-cache bash git',
          'git fetch --tags',
          "latest_version=$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' | sort -V | tail -n 1 | sed 's/v//g')",
          'RELEASE_TAG=$(./tools/image-tag)',
          'if [ "$RELEASE_TAG" != "$latest_version" ]; then echo "Current version $RELEASE_TAG is not the latest version of Loki. The latest version is $latest_version" && exit 78; fi',
        ],
      },
      {
        name: 'prepare-helm-chart-update-config',
        image: 'alpine',
        depends_on: ['check-version-is-latest'],
        commands: [
          'apk add --no-cache bash git',
          'git fetch origin --tags',
          'RELEASE_TAG=$(./tools/image-tag)',
          'echo $PLUGIN_CONFIG_TEMPLATE > %s' % configFileName,
          // replace placeholders with RELEASE TAG
          'sed -i -E "s/\\{\\{release\\}\\}/$RELEASE_TAG/g" %s' % configFileName,
        ],
        settings: {
          config_template: { from_secret: helm_chart_auto_update_config_template.name },
        },
      },
      {
        name: 'trigger-helm-chart-update',
        image: drone_updater_plugin_image,
        settings: {
          github_token: {
            from_secret: github_secret.name,
          },
          config_file: configFileName,
        },
        depends_on: ['prepare-helm-chart-update-config'],
      },
    ],
  },
  logql_analyzer(),
  pipeline('docker-driver') {
    trigger+: onTagOrMain,
    steps: [
      {
        name: 'build and push',
        image: 'grafana/loki-build-image:%s' % build_image_version,
        depends_on: ['clone'],
        environment: {
          DOCKER_USERNAME: { from_secret: docker_username_secret.name },
          DOCKER_PASSWORD: { from_secret: docker_password_secret.name },
        },
        commands: [
          'git fetch origin --tags',
          'make docker-driver-push',
        ],
        volumes: [
          {
            name: 'docker',
            path: '/var/run/docker.sock',
          },
        ],
        privileged: true,
      },
    ],
    volumes: [
      {
        name: 'docker',
        host: {
          path: '/var/run/docker.sock',
        },
      },
    ],
  },
]
+ [
  lambda_promtail(arch)
  for arch in ['amd64', 'arm64']
] + [
  manifest_ecr(['lambda-promtail'], ['amd64', 'arm64']) {
    trigger+: { event: ['push'] },
  },
] + [
  github_secret,
  pull_secret,
  docker_username_secret,
  docker_password_secret,
  ecr_key,
  ecr_secret_key,
  updater_config_template,
  helm_chart_auto_update_config_template,
  gpg_passphrase,
  gpg_private_key,
]
