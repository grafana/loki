local overrides = {
  logcli: {
    description:
      |||
        LogCLI is the command-line interface to Loki.
        It facilitates running LogQL queries against a Loki instance.
      |||,
  },

  'loki-canary': {
    description: 'Loki Canary is a standalone app that audits the log-capturing performance of a Grafana Loki cluster.',
  },

  loki: {
    description: |||
      Loki is a horizontally-scalable, highly-available, multi-tenant log aggregation system inspired by Prometheus.
      It is designed to be very cost effective and easy to operate.
      It does not index the contents of the logs, but rather a set of labels for each log stream.
    |||,
    contents+: [
      {
        src: './tools/packaging/loki.service',
        dst: '/etc/systemd/system/loki.service',
      },
      {
        src: './cmd/loki/loki-local-config.yaml',
        dst: '/etc/loki/config.yml',
        type: 'config|noreplace',
      },
    ],
    scripts: {
      postinstall: './tools/packaging/loki-postinstall.sh',
    },
  },

  promtail: {
    description: |||
      Promtail is an agent which ships the contents of local logs to a private Grafana Loki instance or Grafana Cloud.
      It is usually deployed to every machine that has applications needed to be monitored.
    |||,
    license: 'Apache-2.0',
    contents+: [
      {
        src: './tools/packaging/promtail.service',
        dst: '/etc/systemd/system/promtail.service',
      },
      {
        src: './tools/packaging/promtail-minimal-config.yaml',
        dst: '/etc/promtail/config.yml',
        type: 'config|noreplace',
      },
    ],
    scripts: {
      postinstall: './tools/packaging/promtail-postinstall.sh',
    },
  },
};

local name = std.extVar('name');
local arch = std.extVar('arch');

{
  name: name,
  arch: arch,
  platform: 'linux',
  version: '${DRONE_TAG}',
  section: 'default',
  provides: [name],
  maintainer: 'Grafana Labs <support@grafana.com>',
  vendor: 'Grafana Labs Inc',
  homepage: 'https://grafana.com/loki',
  license: 'AGPL-3.0',
  contents: [{
    src: './dist/tmp/packages/%s-linux-%s' % [name, arch],
    dst: '/usr/bin/%s' % name,
  }],

  deb: {
    signature: {
      // Also set ${NFPM_PASSPHRASE}
      key_file: '${NFPM_SIGNING_KEY_FILE}',
    },
  },
  rpm: {
    signature: {
      // Also set ${NFPM_PASSPHRASE}
      key_file: '${NFPM_SIGNING_KEY_FILE}',
    },
  },
} + overrides[name]
