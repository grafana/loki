type Image = {
  file: string
  platform: string
  version: Version
}
export function buildCommands(repo: string, files: string[]): string[] {
  const commands: string[] = []
  const images = new Map<string, Image[]>()

  for (const file of files) {
    const imageMeta = parseImageMeta(file)
    if (!imageMeta) {
      continue
    }
    const { image, version, platform } = imageMeta

    const platforms = images.get(image) || []
    platforms.push({
      file,
      platform,
      version
    })

    images.set(`${image}`, platforms)
  }

  for (const image of images.keys()) {
    const platforms = images.get(image) || []
    const manifests = []
    let version: Version = {
      major: 0,
      minor: 0,
      patch: 0,
      toString: () => '0.0.0'
    }
    for (const p of platforms) {
      const { file, platform, version: v } = p
      if (version.toString() === '0.0.0') {
        version = v
      }
      const shortPlatform = platform.split('/')[1]
      commands.push(`docker load -i ${file}`)
      manifests.push(`${repo}/${image}:${version.toString()}-${shortPlatform}`)
    }

    commands.push(
      `docker push -a ${repo}/${image}`,
      `docker manifest create ${repo}/${image}:${version.toString()} ${manifests.join(' ')}`,
      `docker manifest push ${repo}/${image}:${version.toString()}`
    )
  }

  return commands
}

type Version = {
  major: number
  minor: number
  patch: number
  preRelease?: string
  toString: () => string
}

const imagePattern =
  /^(?<image>[^0-9]*)-(?<major>0|[1-9]\d*)\.(?<minor>0|[1-9]\d*)\.(?<patch>0|[1-9]\d*)(\.(?<preRelease>[a-zA-Z0-9.]*))?-(?<platform>.*).tar$/
export function parseImageMeta(file: string): {
  image: string
  version: Version
  platform: string
} | null {
  const match = file?.match(imagePattern)

  if (match && match.groups) {
    const { image, major, minor, preRelease, patch, platform } = match.groups
    return {
      image,
      version: parseVersion(major, minor, patch, preRelease),
      platform: platform.replace('-', '/')
    }
  }

  return null
}

function parseVersion(
  maj: string,
  min: string,
  pat: string,
  preRelease?: string
): Version {
  const major = parseInt(maj)
  const minor = parseInt(min)
  const patch = parseInt(pat)

  return {
    major,
    minor,
    patch,
    toString: () => {
      if (preRelease) {
        return `${major}.${minor}.${patch}.${preRelease}`
      }

      return `${major}.${minor}.${patch}`
    }
  }
}
