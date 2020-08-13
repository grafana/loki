local d = import 'doc-util/main.libsonnet';

local fn = {
  mapContainers(f):: {
    local podContainers = super.spec.template.spec.containers,
    spec+: {
      template+: {
        spec+: {
          containers: std.map(f, podContainers),
        },
      },
    },
  },
  mapContainersWithName(names, f)::
    local nameSet = if std.type(names) == 'array' then std.set(names) else std.set([names]);
    local inNameSet(name) = std.length(std.setInter(nameSet, std.set([name]))) > 0;

    self.mapContainers(function(c) if std.objectHas(c, 'name') && inNameSet(c.name) then f(c) else c),
};

local patch = {
  '#mapContainers': d.fn(
    |||
      `mapContainers` applies the function f to each container.
      It works exactly as `std.map`, but on the containers of this object.

      **Signature of `f`**:
      ```ts
      f(container: Object) Object
      ```
    |||,
    [d.arg('f', d.T.func)]
  ),
  mapContainers(f):: fn.mapContainers(f),

  '#mapContainersWithName': d.fn('', [d.arg('names', d.T.array), d.arg('f', d.T.func)]),
  mapContainersWithName(names, f):: fn.mapContainersWithName(names, f),
};

local cronFn = fn {
  mapContainers(f):: {
    local podContainers = super.spec.jobTemplate.spec.template.spec.containers,
    spec+: {
      jobTemplate+: {
        spec+: {
          template+: {
            spec+: {
              containers: std.map(f, podContainers),
            },
          },
        },
      },
    },
  },
};
local cronPatch = patch {
  mapContainers(f):: cronFn.mapContainers(f),
  mapContainersWithName(names, f):: cronFn.mapContainersWithName(names, f),
};

{
  core+: { v1+: {
    pod+: patch,
    podTemplate+: patch,
    replicationController+: patch,
  } },
  batch+: {
    v1+: {
      job+: patch,
    },
    v1beta1+: {
      cronJob+: cronPatch,
    },
  },
  apps+: { v1+: {
    daemonSet+: patch,
    deployment+: patch,
    replicaSet+: patch,
    statefulSet+: patch,
  } },
}
