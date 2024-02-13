import { createGitHubInstance } from '../src/github'
import * as sinon from 'sinon'
import * as core from '@actions/core'

const sandbox = sinon.createSandbox()

describe('github', () => {
  describe('createGitHubInstance', () => {
    it('gets config from the action inputs', async () => {
      const getInputMock = sandbox.stub(core, 'getInput')
      getInputMock.withArgs('repoUrl').returns('test-owner/test-repo')
      getInputMock.withArgs('token').returns('super-secret-token')

      const gh = await createGitHubInstance('main')

      expect(gh).toBeDefined()
      expect(gh.repository).toEqual({
        owner: 'test-owner',
        repo: 'test-repo',
        defaultBranch: 'main'
      })
    })
  })

  //TODO
  describe('findMergedReleasePullRequests', () => {})
})
