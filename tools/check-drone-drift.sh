#!/usr/bin/env bash

set -uo pipefail

command -v drone >/dev/null 2>&1 || { echo "drone is not installed"; exit 1; }

TARGET_BRANCH="$1"
DRONE_JSONNET_FILE=".drone/drone.jsonnet"
DRONE_CONFIG_FILE=".drone/drone.yml"

# Check for a drift between the jsonnet and the resulting file consumed by Drone
modified_drone_jsonnet=$(git diff "${TARGET_BRANCH}" -- "${DRONE_JSONNET_FILE}" | wc -l)
  if [[ "${modified_drone_jsonnet}" -gt "0" ]]; then
    modified_drone_config=$(git diff "${TARGET_BRANCH}" -- "${DRONE_CONFIG_FILE}" | wc -l)
    if [[ "${modified_drone_config}" -lt "1" ]]; then
      echo "There is a drift between ${DRONE_JSONNET_FILE} and ${DRONE_CONFIG_FILE}"
      echo "You can fix it by running:"
      echo "make drone"
      exit 1
    fi
  fi
