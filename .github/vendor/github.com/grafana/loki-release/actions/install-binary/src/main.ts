import { getInput, info, setFailed } from '@actions/core'
import { exec } from '@actions/exec'
import { installTool } from './install'

/**
 * The main function for the action.
 * @returns {Promise<void>} Resolves when the action is complete.
 */
export async function run(): Promise<void> {
  try {
    const binary = getInput('binary')
    const version = getInput('version')
    const _downloadURL = getInput('download_url')
    const _tarballBinaryPath = getInput('tarball_binary_path')
    const _smokeTest = getInput('smoke_test')
    const tarArgs = getInput('tar_args')

    const fillTemplate = (s: string): string => {
      s = s.replace(/\$\{binary\}/g, binary)
      s = s.replace(/\$\{version\}/g, version)
      return s
    }

    const downloadURL = fillTemplate(_downloadURL)
    const tarballBinaryPath = fillTemplate(_tarballBinaryPath)
    const smokeTest = fillTemplate(_smokeTest)

    info(`download URL:         ${downloadURL}`)
    info(`tarball binary path:  ${tarballBinaryPath}`)
    info(`smoke test:           ${smokeTest}`)

    const stripComponents = tarballBinaryPath.split('/').length - 1

    await installTool(
      binary,
      version,
      downloadURL,
      stripComponents,
      tarballBinaryPath,
      tarArgs
    )
    await exec(smokeTest)
  } catch (error) {
    // Fail the workflow run if an error occurs
    if (error instanceof Error) setFailed(error.message)
  }
}
