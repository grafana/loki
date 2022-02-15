#!/usr/bin/env bash

set -uo pipefail

command -v drone >/dev/null 2>&1 || { echo "drone is not installed"; exit 1; }

DRONE_JSONNET_FILE=".drone/drone.jsonnet"
DRONE_CONFIG_FILE=".drone/drone.yml"

# Check for a drift between the jsonnet and the resulting file consumed by Drone
drone jsonnet \
    --stream \
    --stdout \
    --format \
    --source "${DRONE_JSONNET_FILE}" \
    --target "${DRONE_CONFIG_FILE}" \
    | diff "${DRONE_CONFIG_FILE}" -

EXIT_STATUS=$?
if [[ "${EXIT_STATUS}" -eq 1 ]]; then
    echo "There is a drift between ${DRONE_JSONNET_FILE} and ${DRONE_CONFIG_FILE}"
    echo "You can fix it by running:"
    echo "drone jsonnet --stream --format --source ${DRONE_JSONNET_FILE} --target ${DRONE_CONFIG_FILE}"
else
    echo "${DRONE_CONFIG_FILE} is up to date"
fi

exit "${EXIT_STATUS}"
