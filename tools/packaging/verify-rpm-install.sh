#!/bin/sh

docker ps
image="$(docker ps --filter ancestor=jrei/systemd-centos:8 --latest --format "{{.ID}}")"
echo "Running on container: ${image}"

dir="."
if [ -n "${CI}" ]; then
    dir="/drone/src"
fi
echo "Running on directory: ${dir}"

cat <<EOF | docker exec --interactive "${image}" sh
    # Import the Grafana GPG key
    rpm --import https://packages.grafana.com/gpg.key

    # Install loki and check it's running
    rpm -i ${dir}/dist/loki-*.x86_64.rpm
    [ "\$(systemctl is-active loki)" = "active" ] || (echo "loki is inactive" && exit 1)

    # Install promtail and check it's running
    rpm -i ${dir}/dist/promtail-*.x86_64.rpm
    [ "\$(systemctl is-active promtail)" = "active" ] || (echo "promtail is inactive" && exit 1)

    # Write some logs
    mkdir -p /var/log/
    echo "blablabla" >> /var/log/messages

    # Install logcli
    rpm -i ${dir}/dist/logcli-*.x86_64.rpm

    # Check that there are labels
    sleep 5
    labels_found=\$(logcli labels)
    echo "Found labels: \$labels_found"
    [ "\$labels_found" != "" ] || (echo "no labels found with logcli" && exit 1)
EOF
