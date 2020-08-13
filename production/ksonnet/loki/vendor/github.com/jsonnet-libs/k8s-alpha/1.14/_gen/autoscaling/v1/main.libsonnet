{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1', url='', help=''),
  crossVersionObjectReference: (import 'crossVersionObjectReference.libsonnet'),
  horizontalPodAutoscaler: (import 'horizontalPodAutoscaler.libsonnet'),
  horizontalPodAutoscalerSpec: (import 'horizontalPodAutoscalerSpec.libsonnet'),
  horizontalPodAutoscalerStatus: (import 'horizontalPodAutoscalerStatus.libsonnet'),
  scale: (import 'scale.libsonnet'),
  scaleSpec: (import 'scaleSpec.libsonnet'),
  scaleStatus: (import 'scaleStatus.libsonnet')
}