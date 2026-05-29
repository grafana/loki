{
  step: {
    new: function(name, uses=null) {
      name: name,
    } + if uses != null then {
      uses: uses,
    } else {},
    with: function(with) {
      with+: with,
    },
    withRun: function(run) {
      run: run,
    },
    withId: function(id) {
      id: id,
    },
    withWorkingDirectory: function(workingDirectory) {
      'working-directory': workingDirectory,
    },
    withIf: function(_if) {
      'if': _if,
    },
    withEnv: function(env) {
      env: env,
    },
    withSecrets: function(env) {
      secrets: env,
    },
    withTimeoutMinutes: function(timeout) {
      'timeout-minutes': timeout,
    },
    withContinueOnError: function() {
      'continue-on-error': true,
    },
  },
  job: {
    new: function(runsOn='ubuntu-x64') {
      'runs-on': runsOn,
    },
    with: function(with) {
      with+: with,
    },
    withUses: function(uses) {
      uses: uses,
    },
    withSteps: function(steps) {
      steps: steps,
    },
    withStrategy: function(strategy) {
      strategy: strategy,
    },
    withNeeds: function(needs) {
      needs: needs,
    },
    withIf: function(_if) {
      'if': _if,
    },
    withOutputs: function(outputs) {
      outputs: outputs,
    },
    withContainer: function(container) {
      container: container,
    },
    withEnv: function(env) {
      env+: env,
    },
    withSecrets: function(secrets) {
      secrets: secrets,
    },
    withPermissions: function(permissions) {
      permissions+: permissions,
    },
  },

  releaseStep: function(name, uses=null) $.step.new(name, uses) +
                                         $.step.withWorkingDirectory('release'),

  releaseLibStep: function(name, uses=null) $.step.new(name, uses) +
                                            $.step.withWorkingDirectory('lib'),

  checkout:
    $.step.new('checkout', 'actions/checkout@v4')
    + $.step.with({
      'persist-credentials': false,
    }),

  cleanUpBuildCache:
    $.step.new('clean up build tools cache')
    + $.step.withRun('rm -rf /opt/hostedtoolcache'),

  fetchReleaseRepo:
    $.step.new('pull code to release', 'actions/checkout@v4')
    + $.step.with({
      repository: '${{ env.RELEASE_REPO }}',
      path: 'release',
      'persist-credentials': false,
    }),
  fetchReleaseLib:
    $.step.new('pull release library code', 'actions/checkout@v4')
    + $.step.with({
      repository: 'grafana/loki-release',
      path: 'lib',
      ref: '${{ env.RELEASE_LIB_REF }}',
      'persist-credentials': false,
    }),

  setupNode: $.step.new('setup node', 'actions/setup-node@v4')
             + $.step.with({
               'node-version': 24,
               'package-manager-cache': false,
             }),

  // Enable Corepack so the pinned yarn version from package.json's
  // packageManager field is provisioned. Required because the GitHub-hosted
  // runners ship yarn 1.x by default, and `yarn install` / `yarn exec` would
  // otherwise run under yarn 1.x against a yarn 4 lockfile.
  enableCorepack: $.step.new('enable corepack')
                  + $.step.withRun('corepack enable'),

  makeTarget: function(target) 'make %s' % target,

  alwaysGreen: {
    steps: [
      $.step.new('always green')
      + $.step.withRun('echo "always green"'),
    ],
  },

  extractBranchName: $.step.new('extract branch name')
                     + $.step.withId('extract_branch')
                     + $.step.withRun(|||
                       echo "branch=${GITHUB_HEAD_REF:-${GITHUB_REF#refs/heads/}}" >> $GITHUB_OUTPUT
                     |||),

  fixDubiousOwnership: $.step.new('fix git dubious ownership')
                       + $.step.withRun(|||
                         git config --global --add safe.directory "$GITHUB_WORKSPACE"
                       |||),

  githubAppToken: $.step.new('get github app token', 'grafana/shared-workflows/actions/create-github-app-token@580590a644e82e79bb2598bdaba0be245a14dda0')  // create-github-app-token/v0.2.2
                  + $.step.withId('get_github_app_token')
                  + $.step.withIf('${{ fromJSON(env.USE_GITHUB_APP_TOKEN) }}')
                  + $.step.with({
                    github_app: '${{ env.GITHUB_APP }}',
                  }),

  setToken: $.step.new('set github token')
            + $.step.withId('github_app_token')
            + $.step.withEnv({
              OUTPUTS_TOKEN: '${{ steps.get_github_app_token.outputs.token }}',
            })
            + $.step.withRun(|||
              if [[ "${USE_GITHUB_APP_TOKEN}" == "true" ]]; then
                echo "token=$OUTPUTS_TOKEN" >> $GITHUB_OUTPUT
              else
                echo "token=${{ secrets.GH_TOKEN }}" >> $GITHUB_OUTPUT
              fi
            |||),

  validationJob: function(useGCR=false)
    $.job.new()
    + $.job.withContainer({
      image: '${{ inputs.build_image }}',
    } + if useGCR then {
      credentials: {
        username: '_json_key',
        password: '${{ secrets.GCS_SERVICE_ACCOUNT_KEY }}',
      },
    } else {})
    + $.job.withEnv({
      BUILD_IN_CONTAINER: false,
      SKIP_VALIDATION: '${{ inputs.skip_validation }}',
    }),
}
