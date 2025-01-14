#!/bin/bash

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)

docker compose -f "${SCRIPT_DIR}"/docker-compose.yml down  --remove-orphans
