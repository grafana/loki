local g = import '../../lib/grafana.libsonnet';
local promql = g.query.prometheus;
local logql = g.query.loki;

{
  bloom:: (import 'bloom/_imports.libsonnet'),
  cache:: import './cache.libsonnet',
  canary:: import './canary.libsonnet',
  common:: import './common.libsonnet',
  compactor:: import './compactor.libsonnet',
  distributor:: import './distributor.libsonnet',
  gateway:: import './gateway.libsonnet',
  golang:: import './golang.libsonnet',
  ingester:: import './ingester.libsonnet',
  kubernetes:: import './kubernetes/_imports.libsonnet',
  logql:: logql,
  memcache:: import './memcache.libsonnet',
  patterns:: import './patterns.libsonnet',
  promql:: promql,
  querier:: import './querier.libsonnet',
  queryFrontend:: import './query-frontend.libsonnet',
  queryScheduler:: import './query-scheduler.libsonnet',
  queryStats:: import './query-stats.libsonnet',
  requests:: import './requests.libsonnet',
  ruler:: import './ruler.libsonnet',
  tenant:: import './tenant.libsonnet',
}
