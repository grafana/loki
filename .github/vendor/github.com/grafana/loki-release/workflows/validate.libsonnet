local common = import 'common.libsonnet';
local job = common.job;
local step = common.step;
local releaseStep = common.releaseStep;

local setupValidationDeps = function(job) job {
  steps: [
    common.fetchReleaseRepo,
    common.fetchReleaseLib,
    // common.setupGo,
    // common.setupNode,
    // step.new('install dependencies') +
    // step.withRun(|||
    //   go install github.com/bufbuild/buf/cmd/buf@v1.4.0
    //   go install github.com/golang/protobuf/protoc-gen-go@v1.3.1
    //   go install github.com/gogo/protobuf/protoc-gen-gogoslick@v1.3.0
    //   go install github.com/fatih/faillint@v1.11.0
    //   go install golang.org/x/tools/cmd/goimports@v0.7.0
    //   go install github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@v0.4.0
    //   go install github.com/monitoring-mixins/mixtool/cmd/mixtool@bca3066
    //   go install github.com/google/go-jsonnet/cmd/jsonnet@v0.18.0
    //   go install github.com/google/go-jsonnet/cmd/jsonnet-lint@v0.18.0

    //   sudo apt update
    //   sudo apt install -qy musl gnupg ragel \
    //     file zip unzip jq gettext \
    //     protobuf-compiler libprotobuf-dev \
    //     libsystemd-dev jq
    // |||),
    // step.new('install golangci-lint', './lib/actions/install-binary')
    // + step.with({
    //   binary: 'golangci-lint',
    //   version: '1.55.1',
    //   download_url: 'https://github.com/golangci/golangci-lint/releases/download/v${version}/golangci-lint-${version}-linux-amd64.tar.gz',
    //   tarball_binary_path: '*/${binary}',
    //   smoke_test: '${binary} version',
    // }),
    step.new('install shellcheck', './lib/actions/install-binary')
    + step.with({
      binary: 'shellcheck',
      version: '0.9.0',
      download_url: 'https://github.com/koalaman/shellcheck/releases/download/v${version}/shellcheck-v${version}.linux.x86_64.tar.xz',
      tarball_binary_path: '*/${binary}',
      smoke_test: '${binary} --version',
      tar_args: 'xvf',
    }),
    // step.new('install helm', './lib/actions/install-binary')
    // + step.with({
    //   binary: 'helm',
    //   version: '3.2.3',
    //   download_url: 'https://get.helm.sh/helm-v${version}-linux-amd64.tar.gz',
    //   tarball_binary_path: '*/${binary}',
    //   smoke_test: '${binary} version',
    // }),
    // step.new('install helm-docs', './lib/actions/install-binary')
    // + step.with({
    //   binary: 'helm-docs',
    //   version: '1.11.2',
    //   download_url: 'https://github.com/norwoodj/helm-docs/releases/download/v${version}/helm-docs_Linux_x86_64.tar.gz',
    //   tarball_binary_path: '${binary}',
    //   smoke_test: '${binary} --version',
    // }),
    // step.new('install jsonnetfmt', './lib/actions/install-binary')
    // + step.with({
    //   binary: 'jsonnetfmt',
    //   version: '0.18.0',
    //   download_url: 'https://github.com/google/go-jsonnet/releases/download/v${version}/go-jsonnet_${version}_Linux_x86_64.tar.gz',
    //   tarball_binary_path: '${binary}',
    //   smoke_test: '${binary} --version',
    // }),
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
