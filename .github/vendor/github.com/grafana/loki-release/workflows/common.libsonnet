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
    withSecrets: function(env) {
      secrets: env,
    },
  },

  releaseStep: function(name, uses=null) $.step.new(name, uses) +
                                         $.step.withWorkingDirectory('release'),

  releaseLibStep: function(name, uses=null) $.step.new(name, uses) +
                                            $.step.withWorkingDirectory('lib'),

  checkout:
    $.step.new('checkout', 'actions/checkout@v4'),

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
}
