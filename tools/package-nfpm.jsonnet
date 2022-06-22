local descriptions = {
  logcli: |||
    LogCLI is the command-line interface to Loki.
    It facilitates running LogQL queries against a Loki instance.
  |||,

  'loki-canary': 'Loki Canary is a standalone app that audits the log-capturing performance of a Grafana Loki cluster.',

  loki: |||
    Loki is a horizontally-scalable, highly-available, multi-tenant log aggregation system inspired by Prometheus. 
    It is designed to be very cost effective and easy to operate. 
    It does not index the contents of the logs, but rather a set of labels for each log stream.
  |||,

  promtail: |||
    Promtail is an agent which ships the contents of local logs to a private Grafana Loki instance or Grafana Cloud. 
    It is usually deployed to every machine that has applications needed to be monitored.
  |||,
};

local name = std.extVar('name');
local arch = std.extVar('arch');

{
  name: name,
  arch: arch,
  platform: 'linux',
  version: '${CIRCLE_TAG}',
  section: 'default',
  provides: [name],
  maintainer: 'Grafana Labs <support@grafana.com>',
  description: descriptions[name],
  vendor: 'Grafana Labs Inc',
  homepage: 'https://grafana.com/loki',
  changelog: 'https://github.com/grafana/loki/blob/main/CHANGELOG.md',
  license: 'AGPL-3.0',
  contents: [{
    src: './dist/tmp/packages/%s-linux-%s' % [name, arch],
    dst: '/usr/local/bin/%s' % name,
  }],
}
