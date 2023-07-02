{
  local this = self,

  serviceMonitor: if $._config.create_service_monitor then {
    apiVersion: 'monitoring.coreos.com/v1',
    kind: 'ServiceMonitor',
    metadata: {
      name: 'loki-servicemonitor',
      namespace: $._config.namespace,
    },
    spec: {
      namespaceSelector: {
        matchNames: [$._config.namespace],
      },
      servicesToRelabel:: [
        'compactor',
        'distributor',
        'ingester',
        'querier',
        'ruler',
      ],
      endpoints: [
        {
          port: port.name,
          path: '/metrics',
          [if std.member(self.servicesToRelabel, this[name].spec.selector.name) then 'relabelings']: [{
            sourceLabels: ['job'],
            action: 'replace',
            replacement: $._config.namespace + '/$1',
            targetLabel: 'job',
          }],
        }
        for name in std.objectFields(this)
        if std.isObject($[name]) && std.objectHas(this[name], 'kind') && this[name].kind == 'Service'
        for port in std.filter(function(x) std.length(std.findSubstr('http-metrics', x.name)) > 0, this[name].spec.ports)
      ],
      selector: {
        matchExpressions: [{
          key: 'name',
          operator: 'In',
          values: [
            this[name].spec.selector.name
            for name in std.objectFields(this)
            if std.isObject(this[name]) && std.objectHas(this[name], 'kind') && this[name].kind == 'Service' && std.objectHasAll(this[name].spec.selector, 'name')
          ],
        }],
      },
    },
  } else {},
}
