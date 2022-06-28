#!/bin/sh

docker ps
image="$(docker ps --filter ancestor=jrei/systemd-debian:12 --latest --format {{.ID}})"
echo "Running on container: $image"

dir="."
if [ ! -z "$CI" ]; then
    dir="/drone/src"
fi
echo "Running on directory: $dir"

cat <<EOF | docker exec --interactive $image sh
    # Install loki and check it's running
    dpkg -i ${dir}/dist/loki_0.0.0~rc0_amd64.deb
    [ "\$(systemctl is-active loki)" = "active" ] || exit 1
    # Install promtail and check it's running
    dpkg -i ${dir}/dist/promtail_0.0.0~rc0_amd64.deb
    [ "\$(systemctl is-active promtail)" = "active" ] || exit 1
    # Install logcli
    dpkg -i ${dir}/dist/logcli_0.0.0~rc0_amd64.deb
    # Check that there are logs (from the dpkg install)
    [ \$(logcli query \'{job="varlogs"}\' | wc -l) -gt 0 ] || exit 1
EOF