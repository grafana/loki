local common = import 'common.libsonnet';
local job = common.job;
local step = common.step;
local releaseStep = common.releaseStep;
local releaseLibStep = common.releaseLibStep;

{
  image: function(
    name,
    path,
    context='release',
    platform=[
      'linux/amd64',
      'linux/arm64',
      'linux/arm',
    ]
        )
    job.new()
    + job.withStrategy({
      'fail-fast': true,
      matrix: {
        platform: platform,
      },
    })
    + job.withSteps([
      common.fetchReleaseLib,
      common.fetchReleaseRepo,
      common.setupNode,
      common.googleAuth,

      step.new('Set up QEMU', 'docker/setup-qemu-action@v3'),
      step.new('set up docker buildx', 'docker/setup-buildx-action@v3'),

      releaseStep('parse image platform')
      + step.withId('platform')
      + step.withRun(|||
        mkdir -p images

        platform="$(echo "${{ matrix.platform}}" |  sed  "s/\(.*\)\/\(.*\)/\1-\2/")"
        echo "platform=${platform}" >> $GITHUB_OUTPUT
        echo "platform_short=$(echo ${{ matrix.platform }} | cut -d / -f 2)" >> $GITHUB_OUTPUT
      |||),

      step.new('Build and export', 'docker/build-push-action@v5')
      + step.withTimeoutMinutes('${{ fromJSON(env.BUILD_TIMEOUT) }}')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.withEnv({
        IMAGE_TAG: '${{ needs.version.outputs.version }}',
      })
      + step.with({
        context: context,
        file: 'release/%s/Dockerfile' % path,
        platforms: '${{ matrix.platform }}',
        tags: '${{ env.IMAGE_PREFIX }}/%s:${{ needs.version.outputs.version }}-${{ steps.platform.outputs.platform_short }}' % [name],
        outputs: 'type=docker,dest=release/images/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar' % name,
        'build-args': 'IMAGE_TAG=${{ needs.version.outputs.version }}',
      }),
      step.new('upload artifacts', 'google-github-actions/upload-cloud-storage@v2')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.with({
        path: 'release/images/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar' % name,
        destination: 'loki-build-artifacts/${{ github.sha }}/images',  //TODO: make bucket configurable
        process_gcloudignore: false,
      }),
    ]),

  version:
    job.new()
    + job.withSteps([
      common.fetchReleaseLib,
      common.fetchReleaseRepo,
      common.setupNode,
      common.extractBranchName,
      common.githubAppToken,
      common.setToken,
      releaseLibStep('get release version')
      + step.withId('version')
      + step.withRun(|||
        npm install
        npm exec -- release-please release-pr \
          --consider-all-branches \
          --dry-run \
          --dry-run-output release.json \
          --group-pull-request-title-pattern "chore\${scope}: release\${component} \${version}" \
          --manifest-file .release-please-manifest.json \
          --pull-request-title-pattern "chore\${scope}: release\${component} \${version}" \
          --release-type simple \
          --repo-url "${{ env.RELEASE_REPO }}" \
          --separate-pull-requests false \
          --target-branch "${{ steps.extract_branch.outputs.branch }}" \
          --token "${{ steps.github_app_token.outputs.token }}" \
          --versioning-strategy "${{ env.VERSIONING_STRATEGY }}"

        cat release.json

        if [[ `jq length release.json` -gt 1 ]]; then 
          echo 'release-please would create more than 1 PR, so cannot determine correct version'
          echo "pr_created=false" >> $GITHUB_OUTPUT
          exit 1
        fi

        if [[ `jq length release.json` -eq 0 ]]; then 
          echo "pr_created=false" >> $GITHUB_OUTPUT
        else
          version="$(npm run --silent get-version)"
          echo "Parsed version: ${version}"
          echo "version=${version}" >> $GITHUB_OUTPUT
          echo "pr_created=true" >> $GITHUB_OUTPUT
        fi
      |||),
    ])
    + job.withOutputs({
      version: '${{ steps.version.outputs.version }}',
      pr_created: '${{ steps.version.outputs.pr_created }}',
    }),

  dist: function(buildImage, skipArm=true, useGCR=false, makeTargets=['dist', 'packages'])
    job.new()
    + job.withSteps([
      common.fetchReleaseRepo,
      common.googleAuth,
      common.setupGoogleCloudSdk,
      step.new('get nfpm signing keys', 'grafana/shared-workflows/actions/get-vault-secrets@main')
      + step.withId('get-secrets')
      + step.with({
        common_secrets: |||
          NFPM_SIGNING_KEY=packages-gpg:private-key
          NFPM_PASSPHRASE=packages-gpg:passphrase
        |||,
      }),

      releaseStep('build artifacts')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.withEnv({
        BUILD_IN_CONTAINER: false,
        DRONE_TAG: '${{ needs.version.outputs.version }}',
        IMAGE_TAG: '${{ needs.version.outputs.version }}',
        NFPM_SIGNING_KEY_FILE: 'nfpm-private-key.key',
        SKIP_ARM: skipArm,
      })
      //TODO: the workdir here is loki specific
      + step.withRun(
        (
          if useGCR then |||
            gcloud auth configure-docker
          ||| else ''
        ) +
        |||
          cat <<EOF | docker run \
            --interactive \
            --env BUILD_IN_CONTAINER \
            --env DRONE_TAG \
            --env IMAGE_TAG \
            --env NFPM_PASSPHRASE \
            --env NFPM_SIGNING_KEY \
            --env NFPM_SIGNING_KEY_FILE \
            --env SKIP_ARM \
            --volume .:/src/loki \
            --workdir /src/loki \
            --entrypoint /bin/sh "%s"
            git config --global --add safe.directory /src/loki
            echo "${NFPM_SIGNING_KEY}" > $NFPM_SIGNING_KEY_FILE
            make %s
          EOF
        ||| % [buildImage, std.join(' ', makeTargets)]
      ),

      step.new('upload artifacts', 'google-github-actions/upload-cloud-storage@v2')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.with({
        path: 'release/dist',
        destination: 'loki-build-artifacts/${{ github.sha }}',  //TODO: make bucket configurable
        process_gcloudignore: false,
      }),
    ]),
}
