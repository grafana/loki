{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='webhookThrottleConfig', url='', help='WebhookThrottleConfig holds the configuration for throttling events'),
  '#withBurst':: d.fn(help='ThrottleBurst is the maximum number of events sent at the same moment default 15 QPS', args=[d.arg(name='burst', type=d.T.integer)]),
  withBurst(burst): { burst: burst },
  '#withQps':: d.fn(help='ThrottleQPS maximum number of batches per second default 10 QPS', args=[d.arg(name='qps', type=d.T.integer)]),
  withQps(qps): { qps: qps },
  '#mixin': 'ignore',
  mixin: self
}