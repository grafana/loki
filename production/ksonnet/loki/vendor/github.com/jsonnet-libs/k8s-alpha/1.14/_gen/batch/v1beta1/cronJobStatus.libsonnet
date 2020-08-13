{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='cronJobStatus', url='', help='CronJobStatus represents the current state of a cron job.'),
  '#withActive':: d.fn(help='A list of pointers to currently running jobs.', args=[d.arg(name='active', type=d.T.array)]),
  withActive(active): { active: if std.isArray(v=active) then active else [active] },
  '#withActiveMixin':: d.fn(help='A list of pointers to currently running jobs.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='active', type=d.T.array)]),
  withActiveMixin(active): { active+: if std.isArray(v=active) then active else [active] },
  '#withLastScheduleTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='lastScheduleTime', type=d.T.string)]),
  withLastScheduleTime(lastScheduleTime): { lastScheduleTime: lastScheduleTime },
  '#mixin': 'ignore',
  mixin: self
}