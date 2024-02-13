import { GitHub, OctokitAPIs } from 'release-please/build/src/github'

import { getInput, info, debug } from '@actions/core'
import { PullRequest } from 'release-please/build/src/pull-request'
import {
  DEFAULT_RELEASE_LABELS,
  GITHUB_API_URL,
  GITHUB_GRAPHQL_URL
} from './constants'
import { Logger } from 'release-please'

export interface ProxyOption {
  host: string
  port: number
}

export interface GitHubCreateOptions {
  owner: string
  repo: string
  defaultBranch?: string
  apiUrl?: string
  graphqlUrl?: string
  octokitAPIs?: OctokitAPIs
  token?: string
  logger?: Logger
  proxy?: ProxyOption
}

//TODO: copied from release-please, needs tests
export async function findMergedReleasePullRequests(
  baseBranch: string,
  github: GitHub
): Promise<PullRequest[]> {
  // Find merged release pull requests
  const mergedPullRequests: PullRequest[] = []
  const pullRequestGenerator = github.pullRequestIterator(
    baseBranch,
    'MERGED',
    200,
    true
  )
  for await (const pullRequest of pullRequestGenerator) {
    //TODO: found bug from this logic being flipped, do we have a test for that?
    if (hasAllLabels(DEFAULT_RELEASE_LABELS, pullRequest.labels)) {
      continue
    }

    debug(
      `Found merged pull request #${pullRequest.number}: '${pullRequest.title}' without labeles ${DEFAULT_RELEASE_LABELS} in ${pullRequest.labels}`
    )

    mergedPullRequests.push({
      ...pullRequest
    })
  }

  info(`found ${mergedPullRequests.length} merged release pull requests.`)
  return mergedPullRequests
}

/**
 * Helper to compare if a list of labels fully contains another list of labels
 * @param {string[]} expected List of labels expected to be contained
 * @param {string[]} existing List of existing labels to consider
 */
function hasAllLabels(expected: string[], existing: string[]): boolean {
  const existingSet = new Set(existing)
  for (const label of expected) {
    if (!existingSet.has(label)) {
      return false
    }
  }
  return true
}

export async function createGitHubInstance(
  mainBranch: string
): Promise<GitHub> {
  const options = getGitHubOptions(mainBranch)
  return GitHub.create(options)
}

function getGitHubOptions(mainBranch: string): GitHubCreateOptions {
  const { token, apiUrl, graphqlUrl, repoUrl } = getGitHubInput()
  const [owner, repo] = repoUrl.split('/')

  return {
    apiUrl,
    defaultBranch: mainBranch,
    graphqlUrl,
    owner,
    repo,
    token
  }
}

function getGitHubInput(): {
  repoUrl: string
  apiUrl: string
  graphqlUrl: string
  token: string
} {
  return {
    repoUrl: getInput('repoUrl') || (process.env.GITHUB_REPOSITORY as string),
    apiUrl: GITHUB_API_URL,
    graphqlUrl: GITHUB_GRAPHQL_URL,
    token: getInput('token', { required: true })
  }
}
