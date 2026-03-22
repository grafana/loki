local defaultMapping = {
  'linux/amd64': ['github-hosted-ubuntu-x64-small'],
  'linux/arm64': ['github-hosted-ubuntu-arm64-small'],
  'linux/arm': ['github-hosted-ubuntu-arm64-small'],  // equal to linux/arm/v7
};

{
  mapping:: {},

  withDefaultMapping: function()
    self + {
      mapping:: defaultMapping,
    },

  withMapping: function(m)
    self + {
      mapping:: m,
    },

  forPlatform: function(arch)
    local m = self.mapping;

    if std.objectHasEx(m, arch, true)
    then {
      arch: arch,
      runs_on: m[arch],
    }
    else {
      arch: arch,
      runs_on: ['ubuntu-latest'],
    },
}
