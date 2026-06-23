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
    + job.withPermissions({
      'id-token': 'write',
      contents: 'write',
      'pull-requests': 'write',
    })
    + job.withSteps([
      common.fetchReleaseRepo,
      common.fetchReleaseLib,
      common.setupNode,
      common.enableCorepack,
      common.extractBranchName,
      common.githubAppToken,

      releaseLibStep('release please')
      + step.withId('release')
      + step.withEnv({
        SHA: '${{ github.sha }}',
        OUTPUTS_BRANCH: '${{ steps.extract_branch.outputs.branch }}',
        OUTPUTS_TOKEN: '${{ steps.get_github_app_token.outputs.token }}',
        OUTPUTS_VERSION: '${{ needs.dist.outputs.version }}',
      })
      //TODO make bucket configurable
      //TODO make a type/release in the backport action
      //TODO backport action should not bring over autorelease: pending label
      + step.withRun(|||
        yarn install
        yarn exec -- release-please release-pr \
          --changelog-path "${CHANGELOG_PATH}" \
          --consider-all-branches \
          --group-pull-request-title-pattern "chore\${scope}: Release\${component} \${version}" \
          --label "backport main,autorelease: pending,product-approved" \
          --manifest-file .release-please-manifest.json \
          --pull-request-footer "%s" \
          --pull-request-title-pattern "chore\${scope}: Release\${component} \${version}" \
          --release-as "$(echo $OUTPUTS_VERSION | tr -d '"')" \
          --release-type simple \
          --repo-url "${{ env.RELEASE_REPO }}" \
          --separate-pull-requests false \
          --target-branch "$(echo $OUTPUTS_BRANCH | tr -d '"')" \
          --token "$(echo $OUTPUTS_TOKEN | tr -d '"')" \
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
                 + job.withPermissions({
                   'id-token': 'write',
                 })
                 + job.withSteps([
                   common.fetchReleaseRepo,
                   common.fetchReleaseLib,
                   common.setupNode,
                   common.enableCorepack,
                   common.githubAppToken,

                   step.new('Login to GAR', 'grafana/shared-workflows/actions/login-to-gar@12c87e5aa323694c820c1ff3d8e47e8237e05136'),
                   releaseStep('download binaries')
                   + step.withRun(|||
                     echo "downloading binaries to $(pwd)/dist"
                     mkdir -p dist
                     gcloud artifacts generic download \
                       --project="grafanalabs-dev" \
                       --repository="generic-${{ env.GAR_REPO_SLUG }}-dev" \
                       --location="us" \
                       --package=binaries \
                       --version=$(echo ${SHA} | tr -d '"') \
                       --destination=dist/
                   |||),

                   releaseStep('check if release exists')
                   + step.withId('check_release')
                   + step.withEnv({
                     GH_TOKEN: '${{ steps.get_github_app_token.outputs.token }}',
                     OUTPUTS_NAME: '${{ needs.shouldRelease.outputs.name }}',
                   })
                   + step.withRun(|||
                     set +e
                     isDraft="$(gh release view --json="isDraft" --jq=".isDraft" $(echo $OUTPUTS_NAME | tr -d '"') 2>&1)"
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
                     OUTPUTS_TOKEN: '${{ steps.get_github_app_token.outputs.token }}',
                     OUTPUTS_PR_NUMBER: '${{ needs.shouldRelease.outputs.prNumber }}',
                     SHA: '${{ needs.shouldRelease.outputs.sha }}',
                   })
                   + step.withRun(|||
                     yarn install
                     output=$(yarn exec -- release-please github-release \
                       --draft \
                       --release-type simple \
                       --repo-url "${{ env.RELEASE_REPO }}" \
                       --target-branch "$(echo $OUTPUTS_BRANCH | tr -d '"')" \
                       --token "$(echo $OUTPUTS_TOKEN | tr -d '"')" \
                       --shas-to-tag "$(echo $OUTPUTS_PR_NUMBER | tr -d '"'):$(echo ${SHA} | tr -d '"')" \
                       --pull-request-title-pattern "chore\${scope}: Release\${component} \${version}" \
                       --group-pull-request-title-pattern "chore\${scope}: Release\${component} \${version}")
                     echo "$output"
                     if [[ "$output" == "[]" || -z "$output" ]]; then
                       echo "::error::release-please did not create a release"
                       exit 1
                     fi
                   |||),

                   releaseStep('upload artifacts')
                   + step.withId('upload')
                   + step.withEnv({
                     GH_TOKEN: '${{ steps.get_github_app_token.outputs.token }}',
                     OUTPUTS_NAME: '${{ needs.shouldRelease.outputs.name }}',
                   })
                   + step.withRun(|||
                     gh release upload --clobber $(echo $OUTPUTS_NAME | tr -d '"') dist/*
                   |||),

                   releaseStep('release artifacts')
                   + step.withIf('${{ fromJSON(env.PUBLISH_TO_GCS) }}')
                   + step.withRun(|||
                     echo "downloading binaries to $(pwd)/dist"
                     gcloud artifacts generic upload \
                       --project="grafanalabs-global" \
                       --repository="generic-${{ env.GAR_REPO_SLUG }}-prod" \
                       --location="us" \
                       --source-directory=${{ env.path }}, \
                       --package=binaries \
                       --version=${{ github.sha }}
                   |||)
                   + step.withEnv({
                     path: 'release/dist',
                   }),
                 ])
                 + job.withOutputs({
                   sha: '${{ needs.shouldRelease.outputs.sha }}',
                   name: '${{ needs.shouldRelease.outputs.name }}',
                   isLatest: '${{ needs.shouldRelease.outputs.isLatest }}',
                   draft: '${{ steps.check_release.outputs.draft }}',
                   exists: '${{ steps.check_release.outputs.exists }}',
                 }),

  publishImages: function()
    job.new()
    + job.withNeeds(['createRelease'])
    + job.withPermissions({
      'id-token': 'write',
    })
    + job.withSteps(
      [
        common.fetchReleaseLib,
        step.new('Set up QEMU', 'docker/setup-qemu-action@29109295f81e9208d7d86ff1c6c12d2833863392'),  // v3
        step.new('set up docker buildx', 'docker/setup-buildx-action@b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2'),  //v3
        step.new('Login to DockerHub', 'grafana/shared-workflows/actions/dockerhub-login@ef3a62a3ca4c1a15505b4235a5a51493194da3c7'),  // v1.0.4
        step.new('Login to GAR', 'grafana/shared-workflows/actions/login-to-gar@12c87e5aa323694c820c1ff3d8e47e8237e05136'),  // v1.0.2
        step.new('download images')
        + step.withEnv({
          SHA: '${{ needs.createRelease.outputs.sha }}',
        })
        + step.withRun(|||
          echo "downloading images to $(pwd)/images"
          gcloud artifacts generic download \
            --project="grafanalabs-dev" \
            --repository="generic-${{ env.GAR_REPO_SLUG }}-dev" \
            --location="us" \
            --package=images \
            --version=$(echo ${SHA} | tr -d '"') \
            --destination=images/
        |||),
        step.new('publish docker images', './lib/actions/push-images')
        + step.with({
          imageDir: 'images',
          imagePrefix: '${{ env.IMAGE_PREFIX }}',
          isLatest: '${{ needs.createRelease.outputs.isLatest }}',
        }),
      ]
    ),

  publishDockerPlugins: function(path)
    job.new()
    + job.withNeeds(['createRelease'])
    + job.withPermissions({
      'id-token': 'write',
    })
    + job.withSteps(
      [
        common.fetchReleaseLib,
        common.fetchReleaseRepo,
        step.new('Set up QEMU', 'docker/setup-qemu-action@29109295f81e9208d7d86ff1c6c12d2833863392'),  // v3
        step.new('set up docker buildx', 'docker/setup-buildx-action@b5ca514318bd6ebac0fb2aedd5d36ec1b5c232a2'),  //v3
        step.new('Login to DockerHub', 'grafana/shared-workflows/actions/dockerhub-login@ef3a62a3ca4c1a15505b4235a5a51493194da3c7'),  // v1.0.4
        step.new('Login to GAR', 'grafana/shared-workflows/actions/login-to-gar@12c87e5aa323694c820c1ff3d8e47e8237e05136'),  // v1.0.2
        step.new('download and prepare plugins')
        + step.withEnv({
          SHA: '${{ needs.createRelease.outputs.sha }}',
        })
        + step.withRun(|||
          echo "downloading plugins to $(pwd)/plugins"
          gcloud artifacts generic download \
            --project="grafanalabs-dev" \
            --repository="generic-${{ env.GAR_REPO_SLUG }}-dev" \
            --location="us" \
            --package=plugins \
            --version=$(echo ${SHA} | tr -d '"') \
            --destination=plugins/
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
    + job.withPermissions({
      'id-token': 'write',
      contents: 'write',
    })
    + job.withSteps([
      common.fetchReleaseRepo,
      common.githubAppToken,
      releaseStep('publish release')
      + step.withIf('${{ !fromJSON(needs.createRelease.outputs.exists) || (needs.createRelease.outputs.draft && fromJSON(needs.createRelease.outputs.draft)) }}')
      + step.withEnv({
        GH_TOKEN: '${{ steps.get_github_app_token.outputs.token }}',
        OUTPUTS_NAME: '${{ needs.createRelease.outputs.name }}',
        OUTPUTS_IS_LATEST: '${{ needs.createRelease.outputs.isLatest }}',
      })
      + step.withRun(|||
        gh release edit $(echo $OUTPUTS_NAME | tr -d '"') --draft=false --latest=$(echo $OUTPUTS_IS_LATEST | tr -d '"')
      |||),
    ]) + job.withOutputs({
      name: '${{ needs.createRelease.outputs.name }}',
    }),

  createReleaseBranch: function(branchTemplate='release-v\\${major}.\\${minor}.x')
    job.new()
    + job.withNeeds(['publishRelease'])  // always need createRelease for version info
    + job.withPermissions({
      'id-token': 'write',
      contents: 'write',
    })
    + job.withSteps([
      common.fetchReleaseRepo,
      common.extractBranchName,
      common.githubAppToken,

      releaseStep('create release branch')
      + step.withId('create_branch')
      + step.withEnv({
        GH_TOKEN: '${{ steps.get_github_app_token.outputs.token }}',
        VERSION: '${{ needs.publishRelease.outputs.name }}',
        OUTPUTS_NAME: '${{ needs.publishRelease.outputs.name }}',
        OUTPUTS_BRANCH: '${{ steps.extract_branch.outputs.branch }}',
        OUTPUTS_TOKEN: '${{ steps.get_github_app_token.outputs.token }}',
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
          echo "Creating branch: $BRANCH_NAME from tag: $(echo $OUTPUTS_NAME | tr -d '"')"
          
          # Create branch from the tag
          git fetch --tags
          git checkout "$(echo $OUTPUTS_BRANCH | tr -d '"')"
          git checkout -b $BRANCH_NAME

          # explicity set the github app token to override the release branch protection
          git remote set-url origin "https://x-access-token:$(echo ${OUTPUTS_TOKEN} | tr -d '"')@github.com/${{ env.RELEASE_REPO }}"
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
