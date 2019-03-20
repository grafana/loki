#!/bin/sh

##########################################
# Generate the scrape_config for the
# promtail.sh script and the helm chart.
#
# The scrape_config is built from the
# scrape_config defined in the promtail
# ksonnet library.
#########################################

BASE=$(dirname $0)

target=${1:-shell}

case $target in
    "shell")
        (cd $BASE; jsonnet -e '(import "../production/ksonnet/promtail/scrape_config.libsonnet") + { _config:: { promtail_config: { entry_parser: "<parser>"}}}' | ytools 2>/dev/null)
        ;;
    "helm")
        cat <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ template "promtail.fullname" . }}
  labels:
    app: {{ template "promtail.name" . }}
    chart: {{ template "promtail.chart" . }}
    release: {{ .Release.Name }}
    heritage: {{ .Release.Service }}
data:
  promtail.yaml: |
    scrape_configs:
EOF
        (cd $BASE;
          jsonnet -e '(import "../production/ksonnet/promtail/scrape_config.libsonnet") + { _config:: { promtail_config: { entry_parser: "{{ .Values.promtail.entryParser }}"}}}' \
          | ytools 2>/dev/null \
          | tail -n +3 \
          | awk '{ print "      " $0 }' \
        )
        ;;
    *)
        echo "unknown target. expected 'shell' or 'helm'"
        exit 1
esac
