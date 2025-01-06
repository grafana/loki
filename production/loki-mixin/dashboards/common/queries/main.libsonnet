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
  query_frontend:: import './query-frontend.libsonnet',
  query_scheduler:: import './query-scheduler.libsonnet',
  ruler:: import './ruler.libsonnet',
  tenant:: import './tenant.libsonnet',
}
