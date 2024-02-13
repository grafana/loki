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
      common.setupGo,
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

      common.extractBranchName,
      releaseLibStep('get release version')
      + step.withId('version')
      + step.withRun(|||
        npm install
        npm exec -- release-please release-pr \
          --consider-all-branches \
          --dry-run \
          --dry-run-output release.json \
          --release-type simple \
          --repo-url="${{ env.RELEASE_REPO }}" \
          --target-branch "${{ steps.extract_branch.outputs.branch }}" \
          --token="${{ secrets.GH_TOKEN }}" \
          --versioning-strategy "${{ env.VERSIONING_STRATEGY }}"

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

      step.new('Build and export', 'docker/build-push-action@v5')
      + step.withIf('${{ fromJSON(steps.version.outputs.pr_created) }}')
      + step.with({
        context: context,
        file: 'release/%s/Dockerfile' % path,
        platforms: '${{ matrix.platform }}',
        tags: '${{ env.IMAGE_PREFIX }}/%s:${{ steps.version.outputs.version }}-${{ steps.platform.outputs.platform_short }}' % [name],
        outputs: 'type=docker,dest=release/images/%s-${{ steps.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar' % name,
      }),
      step.new('upload artifacts', 'google-github-actions/upload-cloud-storage@v2')
      + step.withIf('${{ fromJSON(steps.version.outputs.pr_created) }}')
      + step.with({
        path: 'release/images/%s-${{ steps.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar' % name,
        destination: 'loki-build-artifacts/${{ github.sha }}/images',  //TODO: make bucket configurable
        process_gcloudignore: false,
      }),
    ]),

  dist: job.new()
        + job.withSteps([
          common.fetchReleaseRepo,
          common.setupGo,
          common.googleAuth,

          step.new('install dependencies') +
          step.withRun(|||
            go install github.com/mitchellh/gox@9f71238
            go install github.com/bufbuild/buf/cmd/buf@v1.4.0
            go install github.com/golang/protobuf/protoc-gen-go@v1.3.1
            go install github.com/gogo/protobuf/protoc-gen-gogoslick@v1.3.0

            sudo apt update
            sudo apt install -qy musl gnupg ragel \
              file zip unzip jq gettext \
              protobuf-compiler libprotobuf-dev \
              libsystemd-dev jq
          |||),

          releaseStep('build artifacts')
          + step.withRun('make BUILD_IN_CONTAINER=false SKIP_ARM=true dist'),

          step.new('upload build artifacts', 'google-github-actions/upload-cloud-storage@v2')
          + step.with({
            path: 'release/dist',
            destination: 'loki-build-artifacts/${{ github.sha }}',  //TODO: make bucket configurable
            process_gcloudignore: false,
          }),
        ]),
}
