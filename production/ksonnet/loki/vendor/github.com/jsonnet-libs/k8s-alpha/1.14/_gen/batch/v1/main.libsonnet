{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1', url='', help=''),
  job: (import 'job.libsonnet'),
  jobCondition: (import 'jobCondition.libsonnet'),
  jobSpec: (import 'jobSpec.libsonnet'),
  jobStatus: (import 'jobStatus.libsonnet')
}