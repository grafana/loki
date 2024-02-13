/**
 * Unit tests for the action's main functionality, src/main.ts
 *
 * These should be run as if the action was called from a workflow.
 * Specifically, the inputs listed in `action.yml` should be set as environment
 * variables following the pattern `INPUT_<INPUT_NAME>`.
 */

import * as core from '@actions/core'
import * as exec from '@actions/exec'

import * as main from '../src/main'
import * as install from '../src/install'

// Mock the action's main function
const runMock = jest.spyOn(main, 'run')

// Mock the GitHub Actions core library
// let debugMock: jest.SpyInstance
// let setOutputMock: jest.SpyInstance

let errorMock: jest.SpyInstance
let getInputMock: jest.SpyInstance
let setFailedMock: jest.SpyInstance
let installToolMock: jest.SpyInstance
let execMock: jest.SpyInstance

describe('action', () => {
  beforeEach(() => {
    jest.clearAllMocks()

    // debugMock = jest.spyOn(core, 'debug').mockImplementation()
    // setOutputMock = jest.spyOn(core, 'setOutput').mockImplementation()

    errorMock = jest.spyOn(core, 'error').mockImplementation()
    getInputMock = jest.spyOn(core, 'getInput').mockImplementation()
    setFailedMock = jest.spyOn(core, 'setFailed').mockImplementation()
    execMock = jest.spyOn(exec, 'exec').mockImplementation()

    installToolMock = jest.spyOn(install, 'installTool').mockImplementation()

    getInputMock.mockImplementation((name: string): string => {
      switch (name) {
        case 'binary':
          return 'foo'
        case 'version':
          return '1.2.3'
        case 'download_url':
          return 'https://example.com/foo-${version}.tar.gz'
        case 'tarball_binary_path':
          return '*/bin/${binary}'
        case 'smoke_test':
          return '${binary} --version'
        case 'tar_args':
          return 'xvf'
        default:
          return ''
      }
    })
  })

  it('correctly gets and templates inputs', async () => {
    await main.run()
    expect(runMock).toHaveReturned()

    expect(installToolMock).toHaveBeenCalledWith(
      'foo',
      '1.2.3',
      'https://example.com/foo-1.2.3.tar.gz',
      2,
      '*/bin/foo',
      'xvf'
    )

    expect(execMock).toHaveBeenCalledWith('foo --version')
  })

  it('sets a failed status', async () => {
    installToolMock.mockRejectedValue(new Error('install failed'))
    await main.run()
    expect(runMock).toHaveReturned()

    // Verify that all of the core library functions were called correctly
    expect(setFailedMock).toHaveBeenNthCalledWith(1, 'install failed')
    expect(errorMock).not.toHaveBeenCalled()
  })
})
