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
local pullRequestFooter = 'Merging this PR will release the [artifacts](https://console.cloud.google.com/storage/browser/${BUILD_ARTIFACTS_BUCKET}/${SHA}) of ${SHA}';

{
  createReleasePR:
    job.new()
    + job.withSteps([
      common.fetchReleaseRepo,
      common.fetchReleaseLib,
      common.setupNode,
      common.extractBranchName,
      common.githubAppToken,
      common.setToken,

      releaseLibStep('release please')
      + step.withId('release')
      + step.withEnv({
        SHA: '${{ github.sha }}',
        OUTPUTS_BRANCH: '${{ steps.extract_branch.outputs.branch }}',
        OUTPUTS_TOKEN: '${{ steps.github_app_token.outputs.token }}',
        OUTPUTS_VERSION: '${{ needs.dist.outputs.version }}'
      })
      //TODO make bucket configurable
      //TODO make a type/release in the backport action
      //TODO backport action should not bring over autorelease: pending label
      + step.withRun(|||
        npm install
        npm exec -- release-please release-pr \
          --changelog-path "${CHANGELOG_PATH}" \
          --consider-all-branches \
          --group-pull-request-title-pattern "chore\${scope}: release\${component} \${version}" \
          --label "backport main,autorelease: pending,product-approved" \
          --manifest-file .release-please-manifest.json \
          --pull-request-footer "%s" \
          --pull-request-title-pattern "chore\${scope}: release\${component} \${version}" \
          --release-as "$OUTPUTS_VERSION" \
          --release-type simple \
          --repo-url "${{ env.RELEASE_REPO }}" \
          --separate-pull-requests false \
          --target-branch "$OUTPUTS_BRANCH" \
          --token "$OUTPUTS_TOKEN" \
          --dry-run ${{ fromJSON(env.DRY_RUN) }}

      ||| % pullRequestFooter),
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
                   prNumber: '${{ steps.should_release.outputs.prNumber }}',
                   isLatest: '${{ steps.should_release.outputs.isLatest }}',
                   branch: '${{ steps.extract_branch.outputs.branch }}',

                 }),
  createRelease: job.new()
                 + job.withNeeds(['shouldRelease'])
                 + job.withIf('${{ fromJSON(needs.shouldRelease.outputs.shouldRelease) }}')
                 + job.withEnv({
                    SHA: '${{ needs.shouldRelease.outputs.sha }}',
                 })
                 + job.withSteps([
                   common.fetchReleaseRepo,
                   common.fetchReleaseLib,
                   common.setupNode,
                   common.googleAuth,
                   common.setupGoogleCloudSdk,
                   common.githubAppToken,
                   common.setToken,

                   // exits with code 1 if the url does not match
                   // meaning there are no artifacts for that sha
                   // we need to handle this if we're going to run this pipeline on every merge to main
                   releaseStep('download binaries')
                   + step.withRun(|||
                     echo "downloading binaries to $(pwd)/dist"
                     gsutil cp -r gs://${BUILD_ARTIFACTS_BUCKET}/${SHA}/dist .
                   |||),

                   releaseStep('check if release exists')
                   + step.withId('check_release')
                   + step.withEnv({
                     GH_TOKEN: '${{ steps.github_app_token.outputs.token }}',
                     OUTPUTS_NAME: '${{ needs.shouldRelease.outputs.name }}',
                   })
                   + step.withRun(|||
                     set +e
                     isDraft="$(gh release view --json="isDraft" --jq=".isDraft" $OUTPUTS_NAME 2>&1)"
                     set -e
                     if [[ "$isDraft" == "release not found" ]]; then
                       echo "exists=false" >> $GITHUB_OUTPUT
                     else
                       echo "exists=true" >> $GITHUB_OUTPUT
                     fi

                     if [[ "$isDraft" == "true" ]]; then
                       echo "draft=true" >> $GITHUB_OUTPUT
                     fi
                   |||),

                   releaseLibStep('create release')
                   + step.withId('release')
                   + step.withIf('${{ !fromJSON(steps.check_release.outputs.exists) }}')
                   + step.withEnv({
                        OUTPUTS_BRANCH: '${{ needs.shouldRelease.outputs.branch }}',
                        OUTPUTS_TOKEN: '${{ steps.github_app_token.outputs.token }}',
                        OUTPUTS_PR_NUMBER: "${{ needs.shouldRelease.outputs.prNumber }}",
                        SHA: '${{ needs.shouldRelease.outputs.sha }}'
                   })
                   + step.withRun(|||
                     npm install
                     npm exec -- release-please github-release \
                       --draft \
                       --release-type simple \
                       --repo-url "${{ env.RELEASE_REPO }}" \
                       --target-branch "$OUTPUTS_BRANCH" \
                       --token "$OUTPUTS_TOKEN" \
                       --shas-to-tag "$OUTPUTS_PR_NUMBER:${SHA}"
                   |||),

                   releaseStep('upload artifacts')
                   + step.withId('upload')
                   + step.withEnv({
                     GH_TOKEN: '${{ steps.github_app_token.outputs.token }}',
                     OUTPUTS_NAME: '${{ needs.shouldRelease.outputs.name }}',
                   })
                   + step.withRun(|||
                     gh release upload --clobber $OUTPUTS_NAME dist/*
                   |||),

                   step.new('release artifacts', 'google-github-actions/upload-cloud-storage@386ab77f37fdf51c0e38b3d229fad286861cc0d0') // v2
                   + step.withIf('${{ fromJSON(env.PUBLISH_TO_GCS) }}')
                   + step.with({
                     path: 'release/dist',
                     destination: '${{ env.PUBLISH_BUCKET }}',
                     parent: false,
                     process_gcloudignore: false,
                   }),
                 ])
                 + job.withOutputs({
                   sha: '${{ needs.shouldRelease.outputs.sha }}',
                   name: '${{ needs.shouldRelease.outputs.name }}',
                   isLatest: '${{ needs.shouldRelease.outputs.isLatest }}',
                   draft: '${{ steps.check_release.outputs.draft }}',
                   exists: '${{ steps.check_release.outputs.exists }}',
                 }),

  publishImages: function(getDockerCredsFromVault=false, dockerUsername='grafanabot')
    job.new()
    + job.withNeeds(['createRelease'])
    + job.withSteps(
      [
        common.fetchReleaseLib,
        common.googleAuth,
        common.setupGoogleCloudSdk,
        step.new('Set up QEMU', 'docker/setup-qemu-action@29109295f81e9208d7d86ff1c6c12d2833863392'), // v3
        step.new('set up docker buildx', 'docker/setup-buildx-action@b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2'), //v3
      ] + (if getDockerCredsFromVault then [
             step.new('Login to DockerHub (from vault)', 'grafana/shared-workflows/actions/dockerhub-login@fa48192dac470ae356b3f7007229f3ac28c48a25'), // main
           ] else [
             step.new('Login to DockerHub (from secrets)', 'docker/login-action@74a5d142397b4f367a81961eba4e8cd7edddf772') // v3
             + step.with({
               username: dockerUsername,
               password: '${{ secrets.DOCKER_PASSWORD }}',
             }),
           ]) +
      [
        step.new('download images')
        + step.withEnv({
            SHA: '${{ needs.createRelease.outputs.sha }}',
        })
        + step.withRun(|||
          echo "downloading images to $(pwd)/images"
          gsutil cp -r gs://${BUILD_ARTIFACTS_BUCKET}/${SHA}/images .
        |||),
        step.new('publish docker images', './lib/actions/push-images')
        + step.with({
          imageDir: 'images',
          imagePrefix: '${{ env.IMAGE_PREFIX }}',
          isLatest: '${{ needs.createRelease.outputs.isLatest }}',
        }),
      ]
    ),

  publishDockerPlugins: function(path, getDockerCredsFromVault=false, dockerUsername='grafanabot')
    job.new()
    + job.withNeeds(['createRelease'])
    + job.withSteps(
      [
        common.fetchReleaseLib,
        common.fetchReleaseRepo,
        common.googleAuth,
        common.setupGoogleCloudSdk,
        step.new('Set up QEMU', 'docker/setup-qemu-action@29109295f81e9208d7d86ff1c6c12d2833863392'), // v3
        step.new('set up docker buildx', 'docker/setup-buildx-action@b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2'), //v3
      ] + (if getDockerCredsFromVault then [
             step.new('Login to DockerHub (from vault)', 'grafana/shared-workflows/actions/dockerhub-login@fa48192dac470ae356b3f7007229f3ac28c48a25'), // main
           ] else [
             step.new('Login to DockerHub (from secrets)', 'docker/login-action@74a5d142397b4f367a81961eba4e8cd7edddf772') // v3
             + step.with({
               username: dockerUsername,
               password: '${{ secrets.DOCKER_PASSWORD }}',
             }),
           ]) +
      [
        step.new('download and prepare plugins')
        + step.withEnv({
            SHA: '${{ needs.createRelease.outputs.sha }}',
        })
        + step.withRun(|||
          echo "downloading images to $(pwd)/plugins"
          gsutil cp -r gs://${BUILD_ARTIFACTS_BUCKET}/${SHA}/plugins .
          mkdir -p "release/%s"
        ||| % path),
        step.new('publish docker driver', './lib/actions/push-images')
        + step.with({
          imageDir: 'plugins',
          imagePrefix: '${{ env.IMAGE_PREFIX }}',
          isPlugin: true,
          buildDir: 'release/%s' % path,
          isLatest: '${{ needs.createRelease.outputs.isLatest }}',
        }),
      ]
    ),

  publishRelease: function(dependencies=['createRelease'])
    job.new()
    + job.withNeeds(dependencies)
    + job.withSteps([
      common.fetchReleaseRepo,
      common.githubAppToken,
      common.setToken,
      releaseStep('publish release')
      + step.withIf('${{ !fromJSON(needs.createRelease.outputs.exists) || (needs.createRelease.outputs.draft && fromJSON(needs.createRelease.outputs.draft)) }}')
      + step.withEnv({
        GH_TOKEN: '${{ steps.github_app_token.outputs.token }}',
        OUTPUTS_NAME: '${{ needs.createRelease.outputs.name }}',
        OUTPUTS_IS_LATEST: '${{ needs.createRelease.outputs.isLatest }}',
      })
      + step.withRun(|||
        gh release edit $OUTPUTS_NAME --draft=false --latest=$OUTPUTS_IS_LATEST
      |||),
    ]) + job.withOutputs({
      name: '${{ needs.createRelease.outputs.name }}',
    }),

  createReleaseBranch: function(branchTemplate='release-v\\${major}.\\${minor}.x')
    job.new()
    + job.withNeeds(['publishRelease'])  // always need createRelease for version info
    + job.withSteps([
      common.fetchReleaseRepo,
      common.extractBranchName,
      common.githubAppToken,
      common.setToken,

      releaseStep('create release branch')
      + step.withId('create_branch')
      + step.withEnv({
        GH_TOKEN: '${{ steps.github_app_token.outputs.token }}',
        VERSION: '${{ needs.publishRelease.outputs.name }}',
        OUTPUTS_NAME: '${{ needs.publishRelease.outputs.name }}',
        OUTPUTS_BRANCH: '${{ steps.extract_branch.outputs.branch }}',
        OUTPUTS_TOKEN: '${{ steps.github_app_token.outputs.token }}',
      })
      + step.withRun(|||
        # Debug and clean the version variable
        echo "Original VERSION: $VERSION"

        # Remove all quotes (both single and double)
        VERSION=$(echo $VERSION | tr -d '"' | tr -d "'")
        echo "After removing quotes: $VERSION"

        # Extract version without the 'v' prefix if it exists
        VERSION="${VERSION#v}"
        echo "After removing v prefix: $VERSION"

        # Extract major and minor versions
        MAJOR=$(echo $VERSION | cut -d. -f1)
        MINOR=$(echo $VERSION | cut -d. -f2)
        echo "MAJOR: $MAJOR, MINOR: $MINOR"

        # Create branch name from template
        BRANCH_TEMPLATE="%s"
        BRANCH_NAME=${BRANCH_TEMPLATE//\$\{major\}/$MAJOR}
        BRANCH_NAME=${BRANCH_NAME//\$\{minor\}/$MINOR}

        echo "Checking if branch already exists: $BRANCH_NAME"

        # Check if branch exists
        if git ls-remote --heads origin $BRANCH_NAME | grep -q $BRANCH_NAME; then
          echo "Branch $BRANCH_NAME already exists, skipping creation"
          echo "branch_exists=true" >> $GITHUB_OUTPUT
          echo "branch_name=$BRANCH_NAME" >> $GITHUB_OUTPUT
        else
          echo "Creating branch: $BRANCH_NAME from tag: $OUTPUTS_NAME"
          
          # Create branch from the tag
          git fetch --tags
          git checkout "$OUTPUTS_BRANCH"
          git checkout -b $BRANCH_NAME

          # explicity set the github app token to override the release branch protection
          git remote set-url origin "https://x-access-token:${OUTPUTS_TOKEN}@github.com/${{ env.RELEASE_REPO }}"
          git push -u origin $BRANCH_NAME
          
          echo "branch_exists=false" >> $GITHUB_OUTPUT
          echo "branch_name=$BRANCH_NAME" >> $GITHUB_OUTPUT
        fi
      ||| % branchTemplate),
    ])
    + job.withOutputs({
      branchExists: '${{ steps.create_branch.outputs.branch_exists }}',
      branchName: '${{ steps.create_branch.outputs.branch_name }}',
    }),
}
