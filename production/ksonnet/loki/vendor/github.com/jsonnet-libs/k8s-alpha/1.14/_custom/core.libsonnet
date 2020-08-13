local d = import 'doc-util/main.libsonnet';

{
  core+: {
    v1+: {
      configMap+: {
        local withData(data) = if data != {} then super.withData(data) else {},
        withData:: withData,

        '#new': d.fn('new creates a new `ConfigMap` of given `name` and `data`', [d.arg('name', d.T.string), d.arg('data', d.T.object)]),
        new(name, data)::
          super.new(name)
          + super.metadata.withName(name)
          + withData(data),
      },

      container+: {
        '#new': d.fn('new returns a new `container` of given `name` and `image`', [d.arg('name', d.T.string), d.arg('image', d.T.string)]),
        new(name, image):: super.withName(name) + super.withImage(image),
      },

      containerPort+: {
        // using a local here to re-use new, because it is lexically scoped,
        // while `self` is not
        local new(containerPort) = super.withContainerPort(containerPort),
        '#new': d.fn('new returns a new `containerPort`', [d.arg('containerPort', d.T.int)]),
        new:: new,
        '#newNamed': d.fn('newNamed works like `new`, but also sets the `name`', [d.arg('containerPort', d.T.int), d.arg('name', d.T.string)]),
        newNamed(containerPort, name):: new(containerPort) + super.withName(name),
      },

      envVar+: {
        '#new': d.fn('new returns a new `envVar` of given `name` and `value`', [d.arg('name', d.T.string), d.arg('value', d.T.string)]),
        new(name, value):: super.withName(name) + super.withValue(value),

        '#fromSecretRef': d.fn('fromSecretRef creates a `envVar` from a secret reference', [
          d.arg('name', d.T.string),
          d.arg('secretRefName', d.T.string),
          d.arg('secretRefKey', d.T.string),
        ]),
        fromSecretRef(name, secretRefName, secretRefKey)::
          super.withName(name)
          + super.valueFrom.secretKeyRef.withName(secretRefName)
          + super.valueFrom.secretKeyRef.withKey(secretRefKey),

        '#fromFieldPath': d.fn('fromFieldPath creates a `envVar` from a field path', [
          d.arg('name', d.T.string),
          d.arg('fieldPath', d.T.string),
        ]),
        fromFieldPath(name, fieldPath)::
          super.withName(name)
          + super.valueFrom.fieldRef.withFieldPath(fieldPath),
      },

      keyToPath+:: {
        '#new': d.fn('new creates a new `keyToPath`', [d.arg('key', d.T.string), d.arg('path', d.T.string)]),
        new(key, path):: super.withKey(key) + super.withPath(path),
      },

      secret+:: {
        '#new'+: d.func.withArgs([
          d.arg('name', d.T.string),
          d.arg('data', d.T.object),
          d.arg('type', d.T.string, 'Opaque'),
        ]),
        new(name, data, type='Opaque')::
          super.new(name)
          + super.withData(data)
          + super.withType(type),
      },

      service+:: {
        '#new'+: d.func.withArgs([
          d.arg('name', d.T.string),
          d.arg('selector', d.T.object),
          d.arg('ports', d.T.array),
        ]),
        new(name, selector, ports)::
          super.new(name)
          + super.spec.withSelector(selector)
          + super.spec.withPorts(ports),
      },

      servicePort+:: {
        local new(port, targetPort) = super.withPort(port) + super.withTargetPort(targetPort),
        '#new': d.fn('new returns a new `servicePort`', [
          d.arg('port', d.T.int),
          d.arg('targetPort', d.T.any),
        ]),
        new:: new,

        '#newNamed': d.fn('newNamed works like `new`, but also sets the `name`', [
          d.arg('name', d.T.string),
          d.arg('port', d.T.int),
          d.arg('targetPort', d.T.any),
        ]),
        newNamed(name, port, targetPort)::
          new(port, targetPort) + super.withName(name),
      },

      volume+:: {
        '#fromConfigMap': d.fn('Creates a new volume from a `ConfigMap`', [
          d.arg('name', d.T.string),
          d.arg('configMapName', d.T.string),
          d.arg('configMapItems', d.T.array),
        ]),
        fromConfigMap(name, configMapName, configMapItems)::
          super.withName(name)
          + super.configMap.withName(configMapName) + super.configMap.withItems(configMapItems),

        '#fromEmptyDir': d.fn('Creates a new volume of type `emptyDir`', [
          d.arg('name', d.T.string),
          d.arg('emptyDir', d.T.object, {}),
        ]),
        fromEmptyDir(name, emptyDir={})::
          super.withName(name) + { emptyDir: emptyDir },

        '#fromPersistentVolumeClaim': d.fn('Creates a new volume using a `PersistentVolumeClaim`.\n\n**Note**: `emptyDir` should be `claimName`, but this is inherited from `ksonnet-lib`', [
          d.arg('name', d.T.string),
          d.arg('emptyDir', d.T.string),
        ]),
        fromPersistentVolumeClaim(name, emptyDir)::  // <- emptyDir should be claimName, but ksonnet
          super.withName(name) + super.persistentVolumeClaim.withClaimName(emptyDir),

        '#fromHostPath': d.fn('Creates a new volume using a `hostPath`', [
          d.arg('name', d.T.string),
          d.arg('hostPath', d.T.string),
        ]),
        fromHostPath(name, hostPath)::
          super.withName(name) + super.hostPath.withPath(hostPath),

        '#fromSecret': d.fn('Creates a new volume from a `Secret`', [
          d.arg('name', d.T.string),
          d.arg('secretName', d.T.string),
        ]),
        fromSecret(name, secretName)::
          super.withName(name) + super.secret.withSecretName(secretName),
      },

      volumeMount+:: {
        '#new': d.fn('new creates a new `volumeMount`', [
          d.arg('name', d.T.string),
          d.arg('mountPath', d.T.string),
          d.arg('readOnly', d.T.bool),
        ]),
        new(name, mountPath, readOnly)::
          super.withName(name) + super.withMountPath(mountPath) + super.withReadOnly(readOnly),
      },
    },
  },
}
