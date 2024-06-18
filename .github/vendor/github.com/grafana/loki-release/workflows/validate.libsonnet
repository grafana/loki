local common = import 'common.libsonnet';
local job = common.job;
local step = common.step;
local _validationJob = common.validationJob;

local setupValidationDeps = function(job) job {
  steps: [
    common.checkout,
    common.fetchReleaseLib,
    common.fixDubiousOwnership,
    step.new('install tar')
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
  ] + job.steps,
};

local validationJob = _validationJob(false);

{
  local validationMakeStep = function(name, target)
    step.new(name)
    + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
    + step.withRun(common.makeTarget(target)),

  // Test jobs
  collectPackages: job.new()
                   + job.withSteps([
                     common.checkout,
                     common.fixDubiousOwnership,
                     step.new('gather packages')
                     + step.withId('gather-tests')
                     + step.withRun(|||
                       echo "packages=$(find . -path '*_test.go' -printf '%h\n' \
                         | grep -e "pkg/push" -e "integration" -e "operator" -e "lambda-promtail" -e "helm" -v \
                         | cut  -d / -f 2,3 \
                         | uniq \
                         | sort \
                         | jq --raw-input --slurp --compact-output 'split("\n")[:-1]')" >> ${GITHUB_OUTPUT}
                     |||),
                   ])
                   + job.withOutputs({
                     packages: '${{ steps.gather-tests.outputs.packages }}',
                   }),

  integration: validationJob
               + job.withSteps([
                 common.checkout,
                 common.fixDubiousOwnership,
                 validationMakeStep('integration', 'test-integration'),
               ]),

  testPackages: validationJob
                + job.withNeeds(['collectPackages'])
                + job.withStrategy({
                  matrix: {
                    package: '${{fromJson(needs.collectPackages.outputs.packages)}}',
                  },
                })
                + job.withSteps([
                  common.checkout,
                  common.fixDubiousOwnership,
                  step.new('test ${{ matrix.package }}')
                  + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
                  + step.withRun(|||
                    gotestsum -- -covermode=atomic -coverprofile=coverage.txt -p=4 ./${{ matrix.package }}/...
                  |||),
                ]),


  testLambdaPromtail: validationJob
                      + job.withSteps([
                        common.checkout,
                        common.fixDubiousOwnership,
                        step.new('test push package')
                        + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
                        + step.withWorkingDirectory('tools/lambda-promtail')
                        + step.withRun(|||
                          gotestsum -- -covermode=atomic -coverprofile=coverage.txt -p=4 ./...
                        |||),
                      ]),

  testPushPackage: validationJob
                   + job.withSteps([
                     common.checkout,
                     common.fixDubiousOwnership,
                     step.new('test push package')
                     + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
                     + step.withWorkingDirectory('pkg/push')
                     + step.withRun(|||
                       gotestsum -- -covermode=atomic -coverprofile=coverage.txt -p=4 ./...
                     |||),
                   ]),

  // Check / lint jobs
  checkFiles: setupValidationDeps(
    validationJob
    + job.withSteps([
      validationMakeStep('check generated files', 'check-generated-files'),
      validationMakeStep('check mod', 'check-mod'),
      validationMakeStep('check docs', 'check-doc'),
      validationMakeStep('validate example configs', 'validate-example-configs'),
      validationMakeStep('validate dev cluster config', 'validate-dev-cluster-config'),
      validationMakeStep('check example config docs', 'check-example-config-doc'),
      validationMakeStep('check helm reference doc', 'documentation-helm-reference-check'),
      validationMakeStep('check drone drift', 'check-drone-drift'),
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

  faillint:
    validationJob
    + job.withSteps([
      common.checkout,
      common.fixDubiousOwnership,
      step.new('faillint')
      + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
      + step.withRun(|||
        faillint -paths "sync/atomic=go.uber.org/atomic" ./...

      |||),
    ]),

  golangciLint: setupValidationDeps(
    validationJob
    + job.withSteps(
      [
        step.new('golangci-lint', 'golangci/golangci-lint-action@08e2f20817b15149a52b5b3ebe7de50aff2ba8c5')
        + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
        + step.with({
          version: '${{ inputs.golang_ci_lint_version }}',
          'only-new-issues': true,
        }),
      ],
    )
  ),

  lintFiles: setupValidationDeps(
    validationJob
    + job.withSteps(
      [
        validationMakeStep('lint scripts', 'lint-scripts'),
        step.new('check format')
        + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
        + step.withRun(|||
          git fetch origin
          make check-format
        |||),
      ]
    )
  ),

  failCheck: job.new()
             + job.withNeeds([
               'checkFiles',
               'faillint',
               'golangciLint',
               'lintFiles',
               'integration',
               'testLambdaPromtail',
               'testPackages',
               'testPushPackage',
             ])
             + job.withEnv({
               SKIP_VALIDATION: '${{ inputs.skip_validation }}',
             })
             + job.withIf("${{ !fromJSON(inputs.skip_validation) && (cancelled() || contains(needs.*.result, 'cancelled') || contains(needs.*.result, 'failure')) }}")
             + job.withSteps([
               common.checkout,
               step.new('verify checks passed')
               + step.withRun(|||
                 echo "Some checks have failed!"
                 exit 1,
               |||),
             ]),

  check: job.new()
         + job.withNeeds([
           'checkFiles',
           'faillint',
           'golangciLint',
           'lintFiles',
           'integration',
           'testLambdaPromtail',
           'testPackages',
           'testPushPackage',
         ])
         + job.withEnv({
           SKIP_VALIDATION: '${{ inputs.skip_validation }}',
         })
         + job.withSteps([
           common.checkout,
           step.new('checks passed')
           + step.withIf('${{ !fromJSON(env.SKIP_VALIDATION) }}')
           + step.withRun(|||
             echo "All checks passed"
           |||),
         ]),

}
