/* eslint-disable @typescript-eslint/no-explicit-any */
import { GitHub } from 'release-please/build/src/github'

export async function mockGitHub(): Promise<GitHub> {
  return GitHub.create({
    owner: 'fake-owner',
    repo: 'fake-repo',
    defaultBranch: 'main',
    token: 'fake-token'
  })
}
