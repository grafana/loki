#!/usr/bin/env bash

curl -s --header "Content-Type: application/json" \
  --data "{\"build_parameters\":{\"CIRCLE_JOB\": \"deploy\", \"IMAGE_NAMES\": \"$(make print-images)\"}}" \
  --request POST \
  https://circleci.com/api/v1.1/project/github/grafana/deployment_tools/tree/master?circle-token=$CIRCLE_TOKEN
