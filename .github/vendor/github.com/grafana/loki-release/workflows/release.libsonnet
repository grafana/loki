local common = import 'common.libsonnet';
local job = common.job;
local step = common.step;
local releaseStep = common.releaseStep;
local releaseLibStep = common.releaseLibStep;

// DO NOT MODIFY THIS FOOTER TEMPLATE
// This template is matched by the should-release action to detect the correct
// sha to release and pull aritfacts from. If you need to change this, make sure
// to change it in both places.
//TODO: make bucket configurable
local pullRequestFooter = 'Merging this PR will release the [artifacts](https://console.cloud.google.com/storage/browser/loki-build-artifacts/${SHA}) of ${SHA}';

{
  createReleasePR:
    job.new()
    + job.withSteps([
      common.fetchReleaseRepo,
      common.fetchReleaseLib,
      common.setupNode,
      common.extractBranchName,

      releaseLibStep('release please')
      + step.withId('release')
      + step.withEnv({
        SHA: '${{ github.sha }}',
      })
      //TODO make bucket configurable
      //TODO make a type/release in the backport action
      //TODO backport action should not bring over autorelease: pending label
      + step.withRun(|||
        npm install
        echo "Pull request footer: %s"
        npm exec -- release-please release-pr \
          --consider-all-branches \
          --label "backport main,autorelease: pending,type/docs" \
          --pull-request-footer "%s" \
          --release-type simple \
          --repo-url "${{ env.RELEASE_REPO }}" \
          --target-branch "${{ steps.extract_branch.outputs.branch }}" \
          --token "${{ secrets.GH_TOKEN }}" \
          --versioning-strategy "${{ env.VERSIONING_STRATEGY }}" \
          --separate-pull-requests false \
          --debug
      ||| % [pullRequestFooter, pullRequestFooter]),
    ]),

  shouldRelease: job.new()
                 + job.withSteps([
                   common.fetchReleaseRepo,
                   common.fetchReleaseLib,
                   common.extractBranchName,

                   step.new('should a release be created?', './lib/actions/should-release')
                   + step.withId('should_release')
                   + step.with({
                     baseBranch: '${{ steps.extract_branch.outputs.branch }}',
                   }),
                 ])
                 + job.withOutputs({
                   shouldRelease: '${{ steps.should_release.outputs.shouldRelease }}',
                   sha: '${{ steps.should_release.outputs.sha }}',
                   name: '${{ steps.should_release.outputs.name }}',
                   branch: '${{ steps.extract_branch.outputs.branch }}',

                 }),
  createRelease: job.new()
                 + job.withNeeds(['shouldRelease'])
                 + job.withIf('${{ fromJSON(needs.shouldRelease.outputs.shouldRelease) }}')
                 + job.withSteps([
                   common.fetchReleaseRepo,
                   common.fetchReleaseLib,
                   common.setupNode,
                   common.googleAuth,
                   common.setupGoogleCloudSdk,

                   // exits with code 1 if the url does not match
                   // meaning there are no artifacts for that sha
                   // we need to handle this if we're going to run this pipeline on every merge to main
                   releaseStep('download binaries')
                   + step.withRun(|||
                     echo "downloading binaries to $(pwd)/dist"
                     gsutil cp -r gs://loki-build-artifacts/${{ needs.shouldRelease.outputs.sha }}/dist .
                   |||),

                   releaseLibStep('create release')
                   + step.withId('release')
                   + step.withRun(|||
                     npm install
                     npm exec -- release-please github-release \
                       --draft \
                       --release-type simple \
                       --repo-url="${{ env.RELEASE_REPO }}" \
                       --target-branch "${{ needs.shouldRelease.outputs.branch }}" \
                       --token="${{ secrets.GH_TOKEN }}"
                   |||),

                   releaseStep('upload artifacts')
                   + step.withId('upload')
                   + step.withEnv({
                     GH_TOKEN: '${{ secrets.GH_TOKEN }}',
                   })
                   + step.withRun(|||
                     gh release upload ${{ needs.shouldRelease.outputs.name }} dist/*
                     gh release edit ${{ needs.shouldRelease.outputs.name }} --draft=false
                   |||),
                 ])
                 + job.withOutputs({
                   sha: '${{ needs.shouldRelease.outputs.sha }}',
                 }),

  publishImages: function(getDockerCredsFromVault=false, dockerUsername='grafanabot')
    job.new()
    + job.withNeeds(['createRelease'])
    + job.withSteps(
      [
        common.fetchReleaseLib,
        common.googleAuth,
        common.setupGoogleCloudSdk,
        step.new('Set up QEMU', 'docker/setup-qemu-action@v3'),
        step.new('set up docker buildx', 'docker/setup-buildx-action@v3'),
      ] + (if getDockerCredsFromVault then [
             step.new('Login to DockerHub (from vault)', 'grafana/shared-workflows/actions/dockerhub-login@main'),
           ] else [
             step.new('Login to DockerHub (from secrets)', 'docker/login-action@v3')
             + step.with({
               username: dockerUsername,
               password: '${{ secrets.DOCKER_PASSWORD }}',
             }),
           ]) +
      [
        step.new('download images')
        + step.withRun(|||
          echo "downloading images to $(pwd)/images"
          gsutil cp -r gs://loki-build-artifacts/${{ needs.createRelease.outputs.sha }}/images .
        |||),
        step.new('publish docker images', './lib/actions/push-images')
        + step.with({
          imageDir: 'images',
          imagePrefix: '${{ env.IMAGE_PREFIX }}',
        }),
      ]
    ),
}
