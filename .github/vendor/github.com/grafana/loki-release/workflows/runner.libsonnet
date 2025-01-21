local mapping = {
  'linux/amd64': ['github-hosted-ubuntu-x64-small'],
  'linux/arm64': ['github-hosted-ubuntu-arm64-small'],
  'linux/arm': ['github-hosted-ubuntu-arm64-small'],
  'linux/arm/v6': ['github-hosted-ubuntu-arm64-small'],
  'linux/arm/v7': ['github-hosted-ubuntu-arm64-small'],
};

{
  forPlatform: function(arch)
    if std.objectHas(mapping, arch)
    then {
      arch: arch,
      runs_on: mapping[arch],
    }
    else {
      arch: arch,
      runs_on: 'ubuntu-latest',
    },
}
