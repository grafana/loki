#!/bin/bash

KAFKA_VERSION="${KAFKA_VERSION:-3.3.1}"
KAFKA_HOME="/opt/kafka-${KAFKA_VERSION}"

if [ ! -d "${KAFKA_HOME}" ]; then
    echo 'Error: KAFKA_VERSION '$KAFKA_VERSION' not available in this image at '$KAFKA_HOME
    exit 1
fi

cd "${KAFKA_HOME}" || exit 1

# discard all empty/commented lines
sed -e '/^#/d' -e '/^$/d' -i"" config/server.properties

# emulate kafka_configure_from_environment_variables from bitnami/bitnami-docker-kafka
for var in "${!KAFKA_CFG_@}"; do
    key="$(echo "$var" | sed -e 's/^KAFKA_CFG_//g' -e 's/_/\./g' -e 's/.*/\L&/')"
    sed -e '/^'$key'/d' -i"" config/server.properties
    value="${!var}"
    echo "$key=$value" >>config/server.properties
done

sort config/server.properties

exec bin/kafka-server-start.sh config/server.properties
