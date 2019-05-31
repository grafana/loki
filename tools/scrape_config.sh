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
        (cd $BASE; jsonnet -e '((import "../production/ksonnet/promtail/scrape_config.libsonnet") + { _config:: { promtail_config: { pipeline_stages: ["<parser>"]}}}).promtail_config' | ytools 2>/dev/null)
        ;;

    *)
        echo "unknown target. expected 'shell' or 'helm'"
        exit 1
esac
