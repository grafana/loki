#!/bin/sh

sleep 15
docker ps
image="$(docker ps --filter ancestor=jrei/systemd-debian:12 --latest --format {{.ID}})"
echo "Running on container: $image"

cat <<EOF | docker exec --interactive $image sh
    ls .
    ls ./dist
    ls /drone/src


    # Install loki and check it's running
    dpkg -i dist/loki_0.0.0~rc0_amd64.deb
    [ "\$(systemctl is-active loki)" = "active" ] || exit 1
    # Install promtail and check it's running
    dpkg -i dist/promtail_0.0.0~rc0_amd64.deb
    [ "\$(systemctl is-active promtail)" = "active" ] || exit 1
    # Install logcli
    dpkg -i dist/logcli_0.0.0~rc0_amd64.deb
    # Check that there are logs (from the dpkg install)
    [ \$(logcli query \'{job="varlogs"}\' | wc -l) -gt 0 ] || exit 1
EOF