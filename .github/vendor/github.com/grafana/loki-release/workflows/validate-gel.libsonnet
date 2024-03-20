local common = import 'common.libsonnet';
local job = common.job;
local step = common.step;
local _validationJob = common.validationJob;

local setupValidationDeps = function(job) job {
  steps: [
    common.checkout,
    common.fetchReleaseLib,
    common.fixDubiousOwnership,
    step.new('install dependencies')
    + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
    + step.withRun(|||
      apt update
      apt install -qy tar xz-utils
    |||),
    step.new('install shellcheck', './lib/actions/install-binary')
    + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
    + step.with({
      binary: 'shellcheck',
      version: '0.9.0',
      download_url: 'https://github.com/koalaman/shellcheck/releases/download/v${version}/shellcheck-v${version}.linux.x86_64.tar.xz',
      tarball_binary_path: '*/${binary}',
      smoke_test: '${binary} --version',
      tar_args: 'xvf',
    }),
    step.new('install jsonnetfmt', './lib/actions/install-binary')
    + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
    + step.with({
      binary: 'jsonnetfmt',
      version: '0.18.0',
      download_url: 'https://github.com/google/go-jsonnet/releases/download/v${version}/go-jsonnet_${version}_Linux_x86_64.tar.gz',
      tarball_binary_path: '${binary}',
      smoke_test: '${binary} --version',
    }),
  ] + job.steps,
};

local validationJob = _validationJob(true);


{
  local validationMakeStep = function(name, target)
    step.new(name)
    + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
    + step.withRun(common.makeTarget(target)),

  test: setupValidationDeps(
    validationJob
    + job.withSteps([
      validationMakeStep('test', 'test'),
    ])
  ),

  lint: setupValidationDeps(
    validationJob
    + job.withSteps(
      [
        step.new('golangci-lint', 'golangci/golangci-lint-action@08e2f20817b15149a52b5b3ebe7de50aff2ba8c5')
        + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
        + step.with({
          version: '${{ inputs.golang_ci_lint_version }}',
          'only-new-issues': true,
          args: '--skip-files cmd/enterprise-logs/fips.go',
        }),
      ],
    )
  ),

  check: setupValidationDeps(
    validationJob
    + job.withSteps([
      validationMakeStep('build enterprise logs', 'all'),
      validationMakeStep('check docs', 'check-docs'),
    ]) + {
      steps+: [
        step.new('build docs website')
        + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
        + step.withRun(|||
          cat <<EOF | docker run \
            --interactive \
            --env BUILD_IN_CONTAINER \
            --env DRONE_TAG \
            --env IMAGE_TAG \
            --volume .:/src/loki \
            --workdir /src/loki \
            --entrypoint /bin/sh "%s"
            git config --global --add safe.directory /src/loki
            mkdir -p /hugo/content/docs/loki/latest
            cp -r docs/sources/* /hugo/content/docs/loki/latest/
            cd /hugo && make prod
          EOF
        ||| % 'grafana/docs-base:e6ef023f8b8'),
      ],
    }
  ),
}
