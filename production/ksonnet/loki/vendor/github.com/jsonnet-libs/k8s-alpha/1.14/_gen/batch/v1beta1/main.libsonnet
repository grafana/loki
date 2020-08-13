{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1beta1', url='', help=''),
  cronJob: (import 'cronJob.libsonnet'),
  cronJobSpec: (import 'cronJobSpec.libsonnet'),
  cronJobStatus: (import 'cronJobStatus.libsonnet'),
  jobTemplateSpec: (import 'jobTemplateSpec.libsonnet')
}