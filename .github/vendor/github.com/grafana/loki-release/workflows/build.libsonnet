local common = import 'common.libsonnet',
      job = common.job,
      step = common.step,
      releaseStep = common.releaseStep,
      releaseLibStep = common.releaseLibStep;
local runner = import 'runner.libsonnet',
      r = runner.withDefaultMapping();

{
  image: function(
    name,
    path,
    dockerfile='Dockerfile',
    context='release',
    platform=[
      r.forPlatform('linux/amd64'),
      r.forPlatform('linux/arm64'),
      r.forPlatform('linux/arm'),
    ]
        )
    job.new('${{ matrix.runs_on }}')
    + job.withStrategy({
      'fail-fast': true,
      matrix: {
        include: platform,
      },
    })
    + job.withSteps([
      common.fetchReleaseLib,
      common.fetchReleaseRepo,
      common.setupNode,
      common.googleAuth,

      step.new('Set up Docker buildx', 'docker/setup-buildx-action@v3'),

      releaseStep('Parse image platform')
      + step.withId('platform')
      + step.withRun(|||
        mkdir -p images

        platform="$(echo "${{ matrix.arch }}" | sed "s/\(.*\)\/\(.*\)/\1-\2/")"
        echo "platform=${platform}" >> $GITHUB_OUTPUT
        echo "platform_short=$(echo ${{ matrix.arch }} | cut -d / -f 2)" >> $GITHUB_OUTPUT
      |||),

      step.new('Build and export', 'docker/build-push-action@v6')
      + step.withTimeoutMinutes('${{ fromJSON(env.BUILD_TIMEOUT) }}')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.withEnv({
        IMAGE_TAG: '${{ needs.version.outputs.version }}',
      })
      + step.with({
        context: context,
        file: 'release/%s/%s' % [path, dockerfile],
        platforms: '${{ matrix.arch }}',
        tags: '${{ env.IMAGE_PREFIX }}/%s:${{ needs.version.outputs.version }}-${{ steps.platform.outputs.platform_short }}' % [name],
        outputs: 'type=docker,dest=release/images/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar' % name,
        'build-args': 'IMAGE_TAG=${{ needs.version.outputs.version }}',
      }),
      step.new('Upload artifacts', 'google-github-actions/upload-cloud-storage@v2')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.with({
        path: 'release/images/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar' % name,
        destination: '${{ env.BUILD_ARTIFACTS_BUCKET }}/${{ github.sha }}/images',  //TODO: make bucket configurable
        process_gcloudignore: false,
      }),
    ]),


  weeklyImage: function(
    name,
    path,
    dockerfile='Dockerfile',
    context='release',
    platform=[
      r.forPlatform('linux/amd64'),
      r.forPlatform('linux/arm64'),
      r.forPlatform('linux/arm'),
    ]
              )
    job.new('${{ matrix.runs_on }}')
    + job.withStrategy({
      'fail-fast': true,
      matrix: {
        include: platform,
      },
    })
    + job.withOutputs({
      image_name: '${{ steps.weekly-version.outputs.image_name }}',
      image_tag: '${{ steps.weekly-version.outputs.image_version }}',
      image_digest_linux_amd64: '${{ steps.digest.outputs.digest_linux_amd64 }}',
      image_digest_linux_arm64: '${{ steps.digest.outputs.digest_linux_arm64 }}',
      image_digest_linux_arm: '${{ steps.digest.outputs.digest_linux_arm }}',
    })
    + job.withSteps([
      common.fetchReleaseLib,
      common.fetchReleaseRepo,
      common.setupNode,

      step.new('Set up Docker buildx', 'docker/setup-buildx-action@v3'),
      step.new('Login to DockerHub (from Vault)', 'grafana/shared-workflows/actions/dockerhub-login@main'),

      releaseStep('Get weekly version')
      + step.withId('weekly-version')
      + step.withRun(|||
        version=$(./tools/image-tag)
        echo "image_version=$version" >> $GITHUB_OUTPUT
        echo "image_name=${{ env.IMAGE_PREFIX }}/%(name)s" >> $GITHUB_OUTPUT
        echo "image_full_name=${{ env.IMAGE_PREFIX }}/%(name)s:$version" >> $GITHUB_OUTPUT
      ||| % { name: name }),

      releaseStep('Parse image platform')
      + step.withId('platform')
      + step.withRun(|||
        platform="$(echo "${{ matrix.arch }}" | sed "s/\(.*\)\/\(.*\)/\1-\2/")"
        echo "platform=${platform}" >> $GITHUB_OUTPUT
        echo "platform_short=$(echo ${{ matrix.arch }} | cut -d / -f 2)" >> $GITHUB_OUTPUT
      |||),

      step.new('Build and push', 'docker/build-push-action@v6')
      + step.withId('build-push')
      + step.withTimeoutMinutes('${{ fromJSON(env.BUILD_TIMEOUT) }}')
      + step.with({
        context: context,
        file: '%s/%s/%s' % [context, path, dockerfile],
        platforms: '${{ matrix.arch }}',
        provenance: true,
        outputs: 'push-by-digest=true,type=image,name=${{ steps.weekly-version.outputs.image_name }},push=true',
        tags: '${{ steps.weekly-version.outputs.image_name }}',
        'build-args': |||
          IMAGE_TAG=${{ steps.weekly-version.outputs.image_version }}
          GO_VERSION=${{ env.GO_VERSION }}
        |||,
      }),

      releaseStep('Process image digest')
      + step.withId('digest')
      + step.withRun(|||
        arch=$(echo ${{ matrix.arch }} | tr "/" "_")
        echo "digest_$arch=${{ steps.build-push.outputs.digest }}" >> $GITHUB_OUTPUT
      |||),
    ]),

  dockerPlugin: function(
    name,
    path,
    buildImage,
    dockerfile='Dockerfile',
    context='release',
    platform=[
      r.forPlatform('linux/amd64'),
      r.forPlatform('linux/arm64'),
    ]
               )
    job.new('${{ matrix.runs_on }}')
    + job.withStrategy({
      'fail-fast': true,
      matrix: {
        include: platform,
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
        mkdir -p plugins

        platform="$(echo "${{ matrix.arch}}" |  sed  "s/\(.*\)\/\(.*\)/\1-\2/")"
        echo "platform=${platform}" >> $GITHUB_OUTPUT
        echo "platform_short=$(echo ${{ matrix.arch }} | cut -d / -f 2)" >> $GITHUB_OUTPUT
        if [[ "${platform}" == "linux/arm64" ]]; then
          echo "plugin_arch=-arm64" >> $GITHUB_OUTPUT
        else
          echo "plugin_arch=" >> $GITHUB_OUTPUT
        fi
      |||),

      step.new('Build and export', 'docker/build-push-action@v6')
      + step.withTimeoutMinutes('${{ fromJSON(env.BUILD_TIMEOUT) }}')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.with({
        context: context,
        file: 'release/%s/%s' % [path, dockerfile],
        platforms: '${{ matrix.arch }}',
        push: false,
        tags: '${{ env.IMAGE_PREFIX }}/%s:${{ needs.version.outputs.version }}-${{ steps.platform.outputs.platform_short }}' % [name],
        outputs: 'type=local,dest=release/plugins/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }}' % name,
        'build-args': |||
          %s
        ||| % std.rstripChars(std.lines([
          'IMAGE_TAG=${{ needs.version.outputs.version }}',
          'GOARCH=${{ steps.platform.outputs.platform_short }}',
          ('BUILD_IMAGE=%s' % buildImage),
        ]), '\n'),
      }),

      step.new('compress rootfs')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.withRun(|||
        tar -cf release/plugins/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar \
        -C release/plugins/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }} \
        .
      ||| % [name, name]),

      step.new('upload artifacts', 'google-github-actions/upload-cloud-storage@v2')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.with({
        path: 'release/plugins/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar' % name,
        destination: '${{ env.BUILD_ARTIFACTS_BUCKET }}/${{ github.sha }}/plugins',
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

        if [[ -z "${{ env.RELEASE_AS }}" ]]; then
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
        else
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
            --release-as "${{ env.RELEASE_AS }}"
        fi

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
      common.cleanUpBuildCache,
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
            if echo "%s" | grep -q "golang"; then
              /src/loki/.github/vendor/github.com/grafana/loki-release/workflows/install_workflow_dependencies.sh dist
            fi
            make %s
          EOF
        ||| % [buildImage, buildImage, std.join(' ', makeTargets)]
      ),

      step.new('upload artifacts', 'google-github-actions/upload-cloud-storage@v2')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.with({
        path: 'release/dist',
        destination: '${{ env.BUILD_ARTIFACTS_BUCKET }}/${{ github.sha }}',  //TODO: make bucket configurable
        process_gcloudignore: false,
      }),
    ])
    + job.withOutputs({
      version: '${{ needs.version.outputs.version }}',
    }),
}
