import { find, cacheDir, downloadTool } from '@actions/tool-cache'
import { addPath } from '@actions/core'
import { exec } from '@actions/exec'

export async function installTool(
  name: string,
  version: string,
  url: string,
  stripComponents: number,
  wildcard: string,
  tarArgs: string
): Promise<void> {
  let cachedPath = find(name, version)
  if (cachedPath) {
    addPath(cachedPath)
    return
  }

  const path = await downloadTool(url)

  await exec(`mkdir ${name}`)
  await exec(
    `tar -C ${name} -${tarArgs} ${path} --strip-components ${stripComponents} --wildcards ${wildcard}`
  )

  cachedPath = await cacheDir(name, name, version)
  addPath(cachedPath)
}
