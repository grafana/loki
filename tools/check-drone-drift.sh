#!/usr/bin/env bash

set -uo pipefail

command -v drone >/dev/null 2>&1 || { echo "drone is not installed"; exit 1; }

DRONE_JSONNET_FILE=".drone/drone.jsonnet"
DRONE_CONFIG_FILE=".drone/drone.yml"
DRONE_ACTUAL_CONFIG_FILE="$(mktemp)"
DRONE_EXPECTED_CONFIG_FILE="$(mktemp)"

# Check for a drift between the jsonnet and the resulting file consumed by Drone
drone jsonnet \
    --stream \
    --stdout \
    --format \
    -V __build-image-version="${1:-latest}" \
    --source "${DRONE_JSONNET_FILE}" \
    > "${DRONE_EXPECTED_CONFIG_FILE}"
# remove last 5 lines which contain the signature
head -n -5 "${DRONE_CONFIG_FILE}" > "${DRONE_ACTUAL_CONFIG_FILE}"
diff "${DRONE_EXPECTED_CONFIG_FILE}" "${DRONE_ACTUAL_CONFIG_FILE}"

EXIT_STATUS=$?
if [[ "${EXIT_STATUS}" -eq 1 ]]; then
    echo "There is a drift between ${DRONE_JSONNET_FILE} and ${DRONE_CONFIG_FILE}"
    echo "You can fix it by running:"
    echo "make drone"
else
    echo "${DRONE_CONFIG_FILE} is up to date"
fi

exit "${EXIT_STATUS}"
