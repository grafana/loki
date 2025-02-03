local common = import 'common.libsonnet';
local job = common.job;
local step = common.step;
local runner = import 'runner.libsonnet',
      r = runner.withDefaultMapping();

{
  ecrImage: function(
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
    + job.withEnv({
      BUILD_TIMEOUT: 60,
      GO_VERSION: '1.23.5',
      IMAGE_PREFIX: 'public.ecr.aws/grafana',
      RELEASE_LIB_REF: 'main',
      RELEASE_REPO: 'grafana/loki',
    })
    + job.withNeeds(['version'])
    + job.withSteps([
      common.fetchReleaseLib,
      common.fetchReleaseRepo,
      common.setupNode,
      common.googleAuth,

      step.new('Set up Docker buildx', 'docker/setup-buildx-action@v3'),
      step.new('Configure AWS credentials', 'aws-actions/configure-aws-credentials@v4')
      + step.with({
        'aws-access-key-id': '${{ secrets.ECR_ACCESS_KEY }}',
        'aws-secret-access-key': '${{ secrets.ECR_SECRET_KEY }}',
        'aws-region': 'us-east-1',
      }),
      step.new('Login to Amazon ECR Public', 'aws-actions/amazon-ecr-login@v2')
      + step.with({
        'registry-type': 'public',
      }),

      step.new('Parse image platform')
      + step.withId('platform')
      + step.withRun(|||
        mkdir -p images
        platform="$(echo "${{ matrix.arch }}" | sed "s/\(.*\)\/\(.*\)/\1-\2/")"
        echo "platform=${platform}" >> $GITHUB_OUTPUT
        echo "platform_short=$(echo ${{ matrix.arch }} | cut -d / -f 2)" >> $GITHUB_OUTPUT
      |||),

      step.new('Build and export')
      + step.withId('build-push')
      + step.withTimeoutMinutes('${{ fromJSON(env.BUILD_TIMEOUT) }}')
      + step.with({
        context: context,
        file: '%s/%s/%s' % [context, path, dockerfile],
        platforms: '${{ matrix.arch }}',
        provenance: true,
        outputs: 'type=docker,dest=release/images/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar' % name,
        tags: '${{ env.IMAGE_PREFIX }}/%s:${{ needs.version.outputs.version }}-${{ steps.platform.outputs.platform_short }}' % name,
        'build-args': |||
          IMAGE_TAG=${{ needs.version.outputs.version }}
          GO_VERSION=${{ env.GO_VERSION }}
        |||,
      }),

      step.new('Upload artifacts')
      + step.withIf('${{ fromJSON(needs.version.outputs.pr_created) }}')
      + step.new('google-github-actions/upload-cloud-storage@v2')
      + step.with({
        destination: '${{ env.BUILD_ARTIFACTS_BUCKET }}/${{ github.sha }}/images',
        path: 'release/images/%s-${{ needs.version.outputs.version}}-${{ steps.platform.outputs.platform }}.tar' % name,
        process_gcloudignore: false,
      }),
    ]),  
}
