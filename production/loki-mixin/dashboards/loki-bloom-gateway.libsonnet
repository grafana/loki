local raw = (import './dashboard-bloom-gateway.json');
local utils = import 'mixin-utils/utils.libsonnet';

// !--- HOW TO UPDATE THIS DASHBOARD ---!
// 1. Export the dashboard from Grafana as JSON
//    !NOTE: Make sure you collapse all rows but the (first) Overview row.
// 2. Copy the JSON into `dashboard-bloom-gateway.json`
// 3. Delete the `id` and `templating` fields from the JSON
(import 'dashboard-utils.libsonnet') {
  local bloom_gateway_job_matcher = if $._config.meta_monitoring.enabled
  then [utils.selector.re('job', '($namespace)/(bloom-gateway|%s-backend|loki-single-binary)' % $._config.ssd.pod_prefix_matcher)]
  else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-backend' % $._config.ssd.pod_prefix_matcher else 'bloom-gateway'))],
  local bloom_gateway_pod_matcher = if $._config.meta_monitoring.enabled
  then [utils.selector.re('pod', '(bloom-gateway|%s-backend|loki-single-binary)' % $._config.ssd.pod_prefix_matcher)]
  else [utils.selector.re('pod', '%s' % (if $._config.ssd.enabled then '%s-backend.*' % $._config.ssd.pod_prefix_matcher else 'bloom-gateway.*'))],

  grafanaDashboards+:
    {
      local dashboards = self,
      'loki-bloom-gateway.json':
        raw
        {
          local replaceClusterMatchers(expr) =
            // Replace the recording rules cluster label with the per-cluster label
            std.strReplace(
              // Replace the cluster label for equality matchers with the per-cluster label
              std.strReplace(
                // Replace the cluster label for regex matchers with the per-cluster label
                std.strReplace(
                  expr,
                  'cluster=~"$cluster"',
                  $._config.per_cluster_label + '=~"$cluster"'
                ),
                'cluster="$cluster"',
                $._config.per_cluster_label + '="$cluster"'
              ),
              'cluster_job',
              $._config.per_cluster_label + '_job'
            ),

          local matcherStr(matcherId, matcher='job', sep=',') =
            if matcher == 'job' then
              if std.length(bloom_gateway_job_matcher) > 0 then
                std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in bloom_gateway_job_matcher]) + sep
              else error 'no job matchers'
            else if matcher == 'pod' then
              if std.length(bloom_gateway_pod_matcher) > 0 then
                std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in bloom_gateway_pod_matcher]) + sep
              else error 'no pod matchers'
            else error 'matcher must be either job or container',

          local replaceMatchers(expr) =
            std.strReplace(
              std.strReplace(
                std.strReplace(
                  std.strReplace(
                    expr,
                    'pod=~"bloom-gateway.*"',
                    matcherStr('bloom-gateway', matcher='pod', sep='')
                  ),
                  'job="$namespace/bloom-gateway",',
                  matcherStr('bloom-gateway')
                ),
                'job="$namespace/bloom-gateway"',
                std.rstripChars(matcherStr('bloom-gateway'), ',')
              ),
              'job="($namespace)/bloom-gateway"',
              std.rstripChars(matcherStr('bloom-gateway'), ',')
            ),

          panels: [
            p {
              targets: if std.objectHas(p, 'targets') then [
                e {
                  expr: replaceMatchers(replaceClusterMatchers(e.expr)),
                }
                for e in p.targets
              ] else [],
              panels: if std.objectHas(p, 'panels') then [
                sp {
                  targets: if std.objectHas(sp, 'targets') then [
                    spe {
                      expr: replaceMatchers(replaceClusterMatchers(spe.expr)),
                    }
                    for spe in sp.targets
                  ] else [],
                  panels: if std.objectHas(sp, 'panels') then [
                    ssp {
                      targets: if std.objectHas(ssp, 'targets') then [
                        sspe {
                          expr: replaceMatchers(replaceClusterMatchers(sspe.expr)),
                        }
                        for sspe in ssp.targets
                      ] else [],
                    }
                    for ssp in sp.panels
                  ] else [],
                }
                for sp in p.panels
              ] else [],
            }
            for p in super.panels
          ],
        }
        + $.dashboard('Loki / Bloom Gateway', uid='bloom-gateway')
          .addCluster()
          .addNamespace()
          .addLog()
          .addTag(),
    },
}
