local defaultMapping = {
  'linux/amd64': ['ubuntu-x64'],
  'linux/arm64': ['ubuntu-arm64'],
  'linux/arm': ['ubuntu-arm64'],  // equal to linux/arm/v7
  'linux/riscv64': ['ubuntu-26.04-riscv'],  // https://github.com/apps/rise-risc-v-runners
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
      runs_on: ['ubuntu-x64'],
    },
}
