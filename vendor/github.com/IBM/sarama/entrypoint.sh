#!/bin/bash

set -eu
set -o pipefail

KAFKA_VERSION="${KAFKA_VERSION:-3.9.1}"
KAFKA_HOME="/opt/kafka-${KAFKA_VERSION}"

if [ ! -d "${KAFKA_HOME}" ]; then
    echo 'Error: KAFKA_VERSION '$KAFKA_VERSION' not available in this image at '$KAFKA_HOME
    exit 1
fi

cd "${KAFKA_HOME}" || exit 1

# discard all empty/commented lines from default config and copy to /tmp
sed -e '/^#/d' -e '/^$/d' config/server.properties >/tmp/server.properties

echo "########################################################################" >>/tmp/server.properties

# emulate kafka_configure_from_environment_variables from bitnami/bitnami-docker-kafka
for var in "${!KAFKA_CFG_@}"; do
    key="$(echo "$var" | sed -e 's/^KAFKA_CFG_//g' -e 's/_/\./g' -e 's/.*/\L&/')"
    sed -e '/^'$key'/d' -i"" /tmp/server.properties
    value="${!var}"
    echo "$key=$value" >>/tmp/server.properties
done

# use KRaft if KAFKA_VERSION is 4.0.0 or newer
if printf "%s\n4.0.0" "$KAFKA_VERSION" | sort -V | head -n1 | grep -q "^4.0.0$"; then
  # remove zookeeper-only options from server.properties and setup storage
  sed -e '/zookeeper.connect/d' \
      -i /tmp/server.properties
  bin/kafka-storage.sh format -t "$KAFKA_CFG_CLUSTER_ID" -c /tmp/server.properties --ignore-formatted
else
  # remove KRaft-only options from server.properties
  sed -e '/process.roles/d' \
      -e '/controller.quorum.voters/d' \
      -e '/controller.listener.names/d' \
      -e '/cluster.id/d' \
      -i /tmp/server.properties
fi

sort /tmp/server.properties

exec bin/kafka-server-start.sh /tmp/server.properties
