// k-compat.libsonnet provides a compatibility layer between k8s-libsonnet and ksonnet-lib. As ksonnet-lib has been
// abandoned, we consider it deprecated.  This layer will generate a deprecation warning to those that still use it.
local k = import 'k.libsonnet';

k
+ (
  if std.objectHas(k, '__ksonnet')
  then
    std.trace(
      'Deprecated: ksonnet-lib has been abandoned, please consider using https://github.com/jsonnet-libs/k8s-libsonnet.',
      (import 'legacy-types.libsonnet')
      + (import 'legacy-custom.libsonnet')
      + (import 'legacy-noname.libsonnet')({
        new(name=''):: super.new() + (if name != '' then super.mixin.metadata.withName(name) else {}),
      })
    )
  else
    (import 'legacy-subtypes.libsonnet')
    + (import 'legacy-noname.libsonnet')({
      new(name=''):: super.new(name),
    })
)
