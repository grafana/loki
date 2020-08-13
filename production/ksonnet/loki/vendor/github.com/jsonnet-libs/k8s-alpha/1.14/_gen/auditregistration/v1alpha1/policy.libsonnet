{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='policy', url='', help='Policy defines the configuration of how audit events are logged'),
  '#withLevel':: d.fn(help='The Level that all requests are recorded at. available options: None, Metadata, Request, RequestResponse required', args=[d.arg(name='level', type=d.T.string)]),
  withLevel(level): { level: level },
  '#withStages':: d.fn(help='Stages is a list of stages for which events are created.', args=[d.arg(name='stages', type=d.T.array)]),
  withStages(stages): { stages: if std.isArray(v=stages) then stages else [stages] },
  '#withStagesMixin':: d.fn(help='Stages is a list of stages for which events are created.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='stages', type=d.T.array)]),
  withStagesMixin(stages): { stages+: if std.isArray(v=stages) then stages else [stages] },
  '#mixin': 'ignore',
  mixin: self
}