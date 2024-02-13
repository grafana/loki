/* eslint-disable @typescript-eslint/no-unused-vars */
/**
 * Unit tests for src/wait.ts
 */

import * as install from '../src/install'
import * as exec from '@actions/exec'
import * as tc from '@actions/tool-cache'
import * as core from '@actions/core'
import { expect } from '@jest/globals'

const installToolMock = jest.spyOn(install, 'installTool')

let execMock: jest.SpyInstance
let addPathMock: jest.SpyInstance

describe('install.ts', () => {
  beforeEach(() => {
    jest.clearAllMocks()

    execMock = jest.spyOn(exec, 'exec').mockImplementation()
    addPathMock = jest.spyOn(core, 'addPath').mockImplementation()

    jest
      .spyOn(tc, 'cacheDir')
      .mockImplementation(
        async (
          sourceDir: string,
          tool: string,
          version: string,
          arch?: string | undefined
        ) => {
          return `/cache/${sourceDir}/bin/${tool}-${version}`
        }
      )

    jest.spyOn(tc, 'downloadTool').mockImplementation(async (url: string) => {
      return 'foo-1.2.3.tar.gz'
    })

    jest
      .spyOn(tc, 'find')
      .mockImplementation((name: string, version: string) => {
        return ''
      })
  })

  it('makes directory', async () => {
    await install.installTool(
      'foo',
      '1.2.3',
      'https://example.com/foo-1.2.3.tar.gz',
      2,
      '*/bin/foo',
      'xvf'
    )

    expect(installToolMock).toHaveReturned()

    expect(execMock).toHaveBeenCalledWith('mkdir foo')
  })

  it('formats correct tar command', async () => {
    await install.installTool(
      'foo',
      '1.2.3',
      'https://example.com/foo-1.2.3.tar.gz',
      2,
      '*/bin/foo',
      'xvf'
    )

    expect(installToolMock).toHaveReturned()

    expect(execMock).toHaveBeenNthCalledWith(
      2,
      'tar -C foo -xvf foo-1.2.3.tar.gz --strip-components 2 --wildcards */bin/foo'
    )
  })

  it('caches binary and adds to path', async () => {
    await install.installTool(
      'foo',
      '1.2.3',
      'https://example.com/foo-1.2.3.tar.gz',
      2,
      '*/bin/foo',
      'xvf'
    )

    expect(installToolMock).toHaveReturned()

    expect(addPathMock).toHaveBeenCalledWith(`/cache/foo/bin/foo-1.2.3`)
  })
})
