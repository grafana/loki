import { createGitHubInstance, findMergedReleasePullRequests } from './github'

import { PullRequest } from 'release-please/build/src/pull-request'
import { PullRequestTitle } from 'release-please/build/src/util/pull-request-title'
import { PullRequestBody } from 'release-please/build/src/util/pull-request-body'

import { error, warning } from '@actions/core'
import { CheckpointLogger } from 'release-please/build/src/util/logger'

export async function shouldRelease(
  baseBranch: string
): Promise<ReleaseMeta | undefined> {
  const gh = await createGitHubInstance(baseBranch)
  const mergedReleasePRs = await findMergedReleasePullRequests(baseBranch, gh)

  const candidateReleases: ReleaseMeta[] = []
  for (const pullRequest of mergedReleasePRs) {
    if (!pullRequest.sha) {
      //not a PR we can make a release from
      continue
    }

    const release = await prepareSingleRelease(pullRequest)

    if (release !== undefined) {
      candidateReleases.push({
        ...release
      })
    }
  }

  if (candidateReleases.length === 0) {
    return undefined
  }

  if (candidateReleases.length > 1) {
    warning(
      'More than one release candidate found, only releasing the first one. Rerun job to release the next.'
    )
  }

  return candidateReleases[0]
}

type ReleaseMeta = {
  name: string
  sha?: string | undefined
}

const footerPattern =
  /^Merging this PR will release the \[artifacts\]\(.*\) of (?<sha>\S+)$/

async function prepareSingleRelease(
  pullRequest: PullRequest
): Promise<ReleaseMeta | undefined> {
  if (!pullRequest.sha) {
    error('Pull request should have been merged')
    return
  }

  const prTitle = PullRequestTitle.parse(pullRequest.title)
  const version = prTitle?.getVersion()
  if (version === undefined) {
    return
  }

  const pullRequestBody = PullRequestBody.parse(
    pullRequest.body,
    new CheckpointLogger()
  )
  if (!pullRequestBody) {
    error('Could not parse pull request body as a release PR')
    return
  }

  const footer = pullRequestBody.footer
  const match = footer?.match(footerPattern)
  if (!match?.groups?.sha) {
    return
  }

  return {
    name: `v${version.toString()}`,
    sha: match.groups.sha
  }
}
