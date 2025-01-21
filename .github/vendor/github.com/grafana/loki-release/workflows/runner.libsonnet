local mapping = {
  'linux/amd64': 'ubuntu-amd64',
  'linux/arm64': 'ubuntu-arm64',
  'linux/arm': 'ubuntu-arm',
  'linux/arm/v6': 'ubuntu-arm',
  'linux/arm/v7': 'ubuntu-arm',
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
