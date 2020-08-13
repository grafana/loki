{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1alpha1', url='', help=''),
  podPreset: (import 'podPreset.libsonnet'),
  podPresetSpec: (import 'podPresetSpec.libsonnet')
}