import { getInput, info, setFailed, setOutput } from '@actions/core'
import { shouldRelease } from './release'

/**
 * The main function for the action.
 * @returns {Promise<void>} Resolves when the action is complete.
 */
export async function run(): Promise<void> {
  try {
    const baseBranch = getInput('baseBranch')

    info(`baseBranch:            ${baseBranch}`)

    const release = await shouldRelease(baseBranch)

    if (release === undefined) {
      info('nothing to release')
      setOutput('shouldRelease', false)
      return
    }

    info(`releasing ${release.sha} as ${release.name}`)
    setOutput('shouldRelease', true)
    setOutput('sha', JSON.stringify(release.sha))
    setOutput('name', JSON.stringify(release.name))
  } catch (error) {
    // Fail the workflow run if an error occurs
    if (error instanceof Error) setFailed(error.message)
  }
}
