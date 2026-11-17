// legacy-custom.libsonnet retrofits k8s-libsonnet functionality into ksonnet-lib
{
  core+: {
    v1+: {
      configMap+: {
        // allow configMap without data
        new(name, data={})::
          super.new(name, data),
        withData(data)::
          // don't add 'data' key if data={}
          if (data == {}) then {}
          else super.withData(data),
        withDataMixin(data)::
          // don't add 'data' key if data={}
          if (data == {}) then {}
          else super.withDataMixin(data),
      },

      volume+:: {
        // Make items parameter optional from fromConfigMap
        fromConfigMap(name, configMapName, configMapItems=[])::
          {
            configMap+:
              if configMapItems == [] then { items:: null }
              else {},
          }
          + super.fromConfigMap(name, configMapName, configMapItems),

        // Shortcut constructor for secret volumes.
        fromSecret(name, secretName)::
          super.withName(name) +
          super.mixin.secret.withSecretName(secretName),

        // Rename emptyDir to claimName
        fromPersistentVolumeClaim(name='', claimName=''):: super.fromPersistentVolumeClaim(name=name, emptyDir=claimName),
      },

      volumeMount+:: {
        // Override new, such that it doesn't always set readOnly: false.
        new(name, mountPath, readOnly=false)::
          {} + self.withName(name) + self.withMountPath(mountPath) +
          if readOnly
          then self.withReadOnly(readOnly)
          else {},
      },

      containerPort+:: {
        // Shortcut constructor for UDP ports.
        newNamedUDP(name, containerPort)::
          super.newNamed(name=name, containerPort=containerPort) +
          super.withProtocol('UDP'),
      },

      persistentVolumeClaim+:: {
        new(name='')::
          super.new()
          + (if name != ''
             then super.mixin.metadata.withName(name)
             else {}),
      },

      container+:: {
        withEnvMixin(es)::
          // if an envvar has an empty value ("") we want to remove that property
          // because k8s will remove that and then it would always
          // show up as a difference.
          local removeEmptyValue(obj) =
            if std.objectHas(obj, 'value') && std.length(obj.value) == 0 then
              {
                [k]: obj[k]
                for k in std.objectFields(obj)
                if k != 'value'
              }
            else
              obj;
          super.withEnvMixin([
            removeEmptyValue(envvar)
            for envvar in es
          ]),

        withEnvMap(es)::
          self.withEnvMixin([
            $.core.v1.envVar.new(k, es[k])
            for k in std.objectFields(es)
          ]),
      },
    },
  },

  batch+: {
    v1beta1+: {
      cronJob+: {
        new(name='', schedule='', containers=[])::
          super.new()
          + super.mixin.spec.jobTemplate.spec.template.spec.withContainers(containers)
          + (if name != ''
             then
               super.mixin.metadata.withName(name)
               + super.mixin.spec.jobTemplate.spec.template.metadata.withLabels({ name: name })
             else {})
          + (
            if schedule != ''
            then super.mixin.spec.withSchedule(schedule)
            else {}
          ),
      },
    },
  },

  local appsExtentions = {
    daemonSet+: {
      new(name, containers, podLabels={})::
        local labels = podLabels { name: name };
        super.new() +
        super.mixin.metadata.withName(name) +
        super.mixin.spec.template.metadata.withLabels(labels) +
        super.mixin.spec.template.spec.withContainers(containers) +
        // apps.v1 requires an explicit selector:
        super.mixin.spec.selector.withMatchLabels(labels),
    },
    deployment+: {
      new(name, replicas, containers, podLabels={})::
        local labels = podLabels { name: name };
        super.new(name, replicas, containers, labels) +

        // apps.v1 requires an explicit selector:
        super.mixin.spec.selector.withMatchLabels(labels),
    },
    statefulSet+: {
      new(name, replicas, containers, volumeClaims=[], podLabels={})::
        local labels = podLabels { name: name };
        super.new(name, replicas, containers, volumeClaims, labels) +

        // apps.v1 requires an explicit selector:
        super.mixin.spec.selector.withMatchLabels(labels) +

        // remove volumeClaimTemplates if empty
        // (otherwise it will create a diff all the time)
        (
          if std.length(volumeClaims) > 0
          then super.mixin.spec.withVolumeClaimTemplates(volumeClaims)
          else {}
        ),
    },
  },

  apps+: {
    v1beta1+: appsExtentions,
    v1+: appsExtentions,
  },

}
