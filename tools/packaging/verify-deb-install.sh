#!/bin/sh

sleep 15
docker ps
image="$(docker ps -f ancestor="jrei/systemd-debian" --last 1 --format {{.ID}})"
echo "Running image $image"

cat <<EOF | docker exec --interactive loki-shell sh
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