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
  },
  job: {
    new: function(runsOn='ubuntu-latest') {
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
      env: env,
    },
    withSecrets: function(secrets) {
      secrets: secrets,
    },
  },

  releaseStep: function(name, uses=null) $.step.new(name, uses) +
                                         $.step.withWorkingDirectory('release'),

  releaseLibStep: function(name, uses=null) $.step.new(name, uses) +
                                            $.step.withWorkingDirectory('lib'),

  checkout:
    $.step.new('checkout', 'actions/checkout@v4'),

  cleanUpBuildCache:
    $.step.new('clean up build tools cache')
    + $.step.withRun('rm -rf /opt/hostedtoolcache'),

  fetchReleaseRepo:
    $.step.new('pull code to release', 'actions/checkout@v4')
    + $.step.with({
      repository: '${{ env.RELEASE_REPO }}',
      path: 'release',
    }),
  fetchReleaseLib:
    $.step.new('pull release library code', 'actions/checkout@v4')
    + $.step.with({
      repository: 'grafana/loki-release',
      path: 'lib',
      ref: '${{ env.RELEASE_LIB_REF }}',
    }),

  setupNode: $.step.new('setup node', 'actions/setup-node@v4')
             + $.step.with({
               'node-version': 20,
             }),

  makeTarget: function(target) 'make %s' % target,

  alwaysGreen: {
    steps: [
      $.step.new('always green')
      + $.step.withRun('echo "always green"'),
    ],
  },

  googleAuth: $.step.new('auth gcs', 'google-github-actions/auth@v2')
              + $.step.with({
                credentials_json: '${{ secrets.GCS_SERVICE_ACCOUNT_KEY }}',
              }),
  setupGoogleCloudSdk: $.step.new('Set up Cloud SDK', 'google-github-actions/setup-gcloud@v2')
                       + $.step.with({
                         version: '>= 452.0.0',
                       }),

  extractBranchName: $.releaseStep('extract branch name')
                     + $.step.withId('extract_branch')
                     + $.step.withRun(|||
                       echo "branch=${GITHUB_HEAD_REF:-${GITHUB_REF#refs/heads/}}" >> $GITHUB_OUTPUT
                     |||),

  fixDubiousOwnership: $.step.new('fix git dubious ownership')
                       + $.step.withRun(|||
                         git config --global --add safe.directory "$GITHUB_WORKSPACE"
                       |||),

  githubAppToken: $.step.new('get github app token', 'actions/create-github-app-token@v1')
                  + $.step.withId('get_github_app_token')
                  + $.step.withIf('${{ fromJSON(env.USE_GITHUB_APP_TOKEN) }}')
                  + $.step.with({
                    'app-id': '${{ secrets.APP_ID }}',
                    'private-key': '${{ secrets.APP_PRIVATE_KEY }}',
                    // By setting owner, we should get access to all repositories in current owner's installation: https://github.com/marketplace/actions/create-github-app-token#create-a-token-for-all-repositories-in-the-current-owners-installation
                    owner: '${{ github.repository_owner }}',
                  }),

  setToken: $.step.new('set github token')
            + $.step.withId('github_app_token')
            + $.step.withRun(|||
              if [[ "${USE_GITHUB_APP_TOKEN}" == "true" ]]; then
                echo "token=${{ steps.get_github_app_token.outputs.token }}" >> $GITHUB_OUTPUT
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
