local lokiLogs = (import './dashboard-loki-logs.json');

{
  grafanaDashboards+: {
    local dashboards = self,

    'loki-logs.json': {
      local cfg = self,

      showMultiCluster:: true,
      clusterLabel:: 'cluster',

      namespaceType:: 'query',
      namespaceQuery::
        if cfg.showMultiCluster then
          'kube_pod_container_info{cluster="$cluster"}'
        else
          'kube_pod_container_info',

      assert (cfg.namespaceType == 'custom' || cfg.namespaceType == 'query') : "Only types 'query' and 'custom' are allowed for dashboard variable 'namespace'",

      templateLabels:: [
        {
          name:: 'logs',
          type:: 'datasource',
          query:: 'loki',
        },
        {
          name:: 'logmetrics',
          type:: 'datasource',
          query:: 'prometheus',
        },
        {
          name:: 'metrics',
          type:: 'datasource',
          query:: 'prometheus',
        },
      ]+(
        if cfg.showMultiCluster then [
          {
            variable:: 'cluster',
            label:: cfg.clusterLabel,
            query:: 'kube_pod_container_info',
            datasource:: '$metrics',
            type:: 'query',
          },
        ] else []
      ) + [
        {
          variable:: 'namespace',
          label:: 'namespace',
          query:: cfg.namespaceQuery,
          datasource:: '$metrics',
          type:: cfg.namespaceType,
        },
        {
          variable:: 'deployment',
          label:: 'deployment',
          query:: (
            if cfg.showMultiCluster then
            'kube_deployment_created{cluster="$cluster", namespace="$namespace"}'
            else
            'kube_deployment_created{namespace="$namespace"}'
          ),
          datasource:: '$metrics',
          type:: 'query',
        },
        {
          variable:: 'pod',
          label:: 'pod',
          query:: (
            if cfg.showMultiCluster then
            'kube_pod_container_info{cluster="$cluster", namespace="$namespace", pod=~"$deployment.*"}'
            else
            'kube_pod_container_info{namespace="$namespace", pod=~"$deployment.*"}'
          ),
          datasource:: '$metrics',
          type:: 'query',
        },
        {
          variable:: 'container',
          label:: 'container',
          query:: (
            if cfg.showMultiCluster then
            'kube_pod_container_info{cluster="$cluster", namespace="$namespace", pod=~"$pod", pod=~"$deployment.*"}'
            else
            'kube_pod_container_info{namespace="$namespace", pod=~"$pod", pod=~"$deployment.*"}'
          ),
          datasource:: '$metrics',
          type:: 'query',
        },
        {
          name:: 'level',
          type:: 'custom',
          query:: 'debug,info,warn,error',
          allValue:: '.*',
          includeAll:: true,
          multi:: true,
          options:: [
            {
              selected: true,
              text: 'All',
              value: '$__all'
            },
            {
              selected: false,
              text: 'debug',
              value: 'debug'
            },
            {
              selected: false,
              text: 'info',
              value: 'info'
            },
            {
              selected: false,
              text: 'warn',
              value: 'warn'
            },
            {
              selected: false,
              text: 'error',
              value: 'error'
            },
          ],
        },
        {
          label:: 'LogQL Filter',
          name:: 'filter',
          options:: [
            {
              selected: true,
              text: '',
              value: '',
            },
          ],
          query:: '',
          type: 'textbox',
        },
      ],
    } + lokiLogs +
    {
      panels: [
        p + {
          targets: [
            e + {
              expr: if dashboards['loki-logs.json'].showMultiCluster then super.expr
              else std.strReplace(super.expr, 'cluster="$cluster", ', '')
            }
            for e in p.targets
          ],
        }
        for p in super.panels
      ],
      templating: {
        list+: [
          {
            hide: 0,
            includeAll: false,
            label: null,
            multi: false,
            name: l.name,
            options: [],
            query: l.query,
            refresh: 1,
            regex: '',
            skipUrlSync: false,
            type: l.type,
          },
          for l in dashboards['loki-logs.json'].templateLabels
          if l.type == 'datasource'
        ] + [
          {
            allValue: null,
            current: {},
            datasource: l.datasource,
            hide: 0,
            includeAll: false,
            label: l.variable,
            multi: false,
            name: l.variable,
            options: [],
            query: 'label_values(%s, %s)' % [l.query, l.label],
            refresh: 1,
            regex: '',
            sort: 2,
            tagValuesQuery: '',
            tags: [],
            tagsQuery: '',
            type: 'query',
            useTags: false,
          }
          for l in dashboards['loki-logs.json'].templateLabels
          if l.type == 'query'
        ] + [
          {
            allValue: l.allValue,
            hide: 0,
            includeAll: l.includeAll,
            label: null,
            multi: l.multi,
            name: l.name,
            options: l.options,
            query: l.query,
            skipUrlSync: false,
            type: l.type
          }
          for l in dashboards['loki-logs.json'].templateLabels
          if l.type == 'custom'
        ] + [
          {
            hide: 0,
            label: l.label,
            name: l.name,
            options: l.options,
            query: l.query,
            skipUrlSync: false,
            type: l.type,
          }
          for l in dashboards['loki-logs.json'].templateLabels
          if l.type == 'textbox'
        ],
      },
    },
  },
}
