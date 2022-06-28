#!/bin/sh

docker ps
image="$(docker ps --filter ancestor=jrei/systemd-centos:8 --latest --format {{.ID}})"
echo "Running on container: $image"

dir="."
if [ ! -z "$CI" ]; then
    dir="/drone/src"
fi
echo "Running on directory: $dir"

cat <<EOF | docker exec --interactive $image sh
    # Install loki and check it's running
    rpm -i ${dir}/dist/loki-0.0.0~rc0.x86_64.rpm
    [ "\$(systemctl is-active loki)" = "active" ] || (echo "loki is inactive" && exit 1)
    
    # Install promtail and check it's running
    rpm -i ${dir}/dist/promtail-0.0.0~rc0.x86_64.rpm
    [ "\$(systemctl is-active promtail)" = "active" ] || (echo "promtail is inactive" && exit 1)

    # Install logcli
    rpm -i ${dir}/dist/logcli-0.0.0~rc0.x86_64.rpm

    # Check that there are logs (from the dpkg install)
    labels_found=\$(logcli labels)
    echo "Found labels: \$labels_found"
    [ "\$labels_found" != "" ] || (echo "no labels found with logcli" && exit 1)
EOF