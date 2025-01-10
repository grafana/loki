local g = import '../../lib/grafana.libsonnet';
local promql = g.query.prometheus;
local logql = g.query.loki;

{
  blooms:: import './blooms.libsonnet',
  canary:: import './canary.libsonnet',
  common:: import './common.libsonnet',
  compactor:: import './compactor.libsonnet',
  distributor:: import './distributor.libsonnet',
  gateway:: import './gateway.libsonnet',
  ingester:: import './ingester.libsonnet',
  logql:: logql,
  patterns:: import './patterns.libsonnet',
  promql:: promql,
  querier:: import './querier.libsonnet',
  queryFrontend:: import './query-frontend.libsonnet',
  queryScheduler:: import './query-scheduler.libsonnet',
  read:: import './read.libsonnet',
  ruler:: import './ruler.libsonnet',
  tenant:: import './tenant.libsonnet',
}
