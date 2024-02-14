local common = import 'common.libsonnet';
local job = common.job;
local step = common.step;
local releaseStep = common.releaseStep;

local setupValidationDeps = function(job) job {
  steps: [
    common.fetchReleaseRepo,
    common.fetchReleaseLib,
    step.new('install tar') +
    step.withRun(|||
      apt update
      apt install -qy tar xz-utils
    |||),
    step.new('install shellcheck', './lib/actions/install-binary')
    + step.with({
      binary: 'shellcheck',
      version: '0.9.0',
      download_url: 'https://github.com/koalaman/shellcheck/releases/download/v${version}/shellcheck-v${version}.linux.x86_64.tar.xz',
      tarball_binary_path: '*/${binary}',
      smoke_test: '${binary} --version',
      tar_args: 'xvf',
    }),
    step.new('install jsonnetfmt', './lib/actions/install-binary')
    + step.with({
      binary: 'jsonnetfmt',
      version: '0.18.0',
      download_url: 'https://github.com/google/go-jsonnet/releases/download/v${version}/go-jsonnet_${version}_Linux_x86_64.tar.gz',
      tarball_binary_path: '${binary}',
      smoke_test: '${binary} --version',
    }),
  ] + job.steps,
};


function(buildImage) {
  local validationMakeStep = function(name, target)
    releaseStep(name)
    + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
    + step.withEnv({
      BUILD_IN_CONTAINER: false,
    })
    + step.withRun(common.makeTarget(target)),

  test: setupValidationDeps(
    job.new()
    + job.withContainer({
      image: buildImage,
    })
    + job.withSteps([
      validationMakeStep('test', 'test'),
    ])
  ),

  lint: setupValidationDeps(
    job.new()
    + job.withContainer({
      image: buildImage,
    })
    + job.withSteps([
      validationMakeStep('lint', 'lint'),
      validationMakeStep('lint jsonnet', 'lint-jsonnet'),
    ])
  ),

  check: setupValidationDeps(
    job.new()
    + job.withContainer({
      image: buildImage,
    })
    + job.withSteps([
      validationMakeStep('check generated files', 'check-generated-files'),
      validationMakeStep('check mod', 'check-mod'),
      validationMakeStep('shellcheck', 'lint-scripts'),
      validationMakeStep('check docs', 'check-doc'),
      validationMakeStep('validate example configs', 'check-example-config-doc'),
      validationMakeStep('check helm reference doc', 'documentation-helm-reference-check'),
    ])
  ),
}
