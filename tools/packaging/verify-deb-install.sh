#!/bin/sh

docker ps
image="$(docker ps --filter ancestor=jrei/systemd-debian:12 --latest --format "{{.ID}}")"
echo "Running on container: ${image}"

dir="."
if [ -n "${CI}" ]; then
    dir="/drone/src"
fi
echo "Running on directory: ${dir}"

cat <<EOF | docker exec --interactive "${image}" sh
    # Install loki and check it's running
    dpkg -i ${dir}/dist/loki_*_amd64.deb
    [ "\$(systemctl is-active loki)" = "active" ] || (echo "loki is inactive" && exit 1)

    # Install promtail and check it's running
    dpkg -i ${dir}/dist/promtail_*_amd64.deb
    [ "\$(systemctl is-active promtail)" = "active" ] || (echo "promtail is inactive" && exit 1)

    # Write some logs
    mkdir -p /var/log/
    echo "blablabla" >> /var/log/messages

    # Install logcli
    dpkg -i ${dir}/dist/logcli_*_amd64.deb

    # Check that there are labels
    sleep 5
    labels_found=\$(logcli labels)
    echo "Found labels: \$labels_found"
    [ "\$labels_found" != "" ] || (echo "no logs found with logcli" && exit 1)
EOF
