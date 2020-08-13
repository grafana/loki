// importing it here, to avoid receiving patches from extensions like kausal
// like `self` would do
local k = import '../main.libsonnet';

{
  core+: { v1+: {
    container+: {
      envType: k.core.v1.envVar,
      portsType: k.core.v1.containerPort,
      volumeMountsType: k.core.v1.volumeMount,
    },
    pod+: {
      spec+: {
        volumesType: k.core.v1.volume,
      },
    },
    service+: {
      spec+: {
        portsType: k.core.v1.servicePort,
      },
    },
  } },
  apps+: { v1+: {
    deployment+: {
      spec+: {
        template+: {
          spec+: {
            containersType: k.core.v1.container,
          },
        },
      },
    },
  } },
}
