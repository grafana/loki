#! /usr/bin/env bash

set -euo pipefail

if ! docker buildx inspect | grep -E 'Driver:\s+docker-container' >/dev/null; then
  echo "Active buildx builder does not use the docker-container driver, which is required for mutli-architecture image builds. Creating a new buildx builder..."
  docker buildx create --use
fi

