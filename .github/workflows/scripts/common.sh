#!/usr/bin/env bash

set -exo pipefail

# This generates a new file where the yaml node is updated.
# The problem is that yq strips new lines when you update the file.
# So we use a workaround from https://github.com/mikefarah/yq/issues/515 which:
# generates the new file, diffs it with the original, removes all non-whitespace changes, and applies that to the original file.
update_yaml_node() {
  local filename=$1
  local yaml_node=$2
  local new_value=$3
  patch "${filename}" <<<"$(diff -U0 -w -b --ignore-blank-lines "${filename}" <(yq eval "${yaml_node} = \"${new_value}\"" "${filename}"))"
}

get_yaml_node() {
  local filename=$1
  local yaml_node=$2
  yq "${yaml_node}" "${filename}"
}

# Increments the part of the semver string
# $1: version itself
# $2: number of part: 0 – major, 1 – minor, 2 – patch
# shellcheck disable=SC2207,SC2046,SC2248,SC2250
increment_semver() {
  local delimiter=.
  local array=($(echo "$1" | tr $delimiter '\n'))
  array[$2]=$((array[$2]+1))
  echo $(local IFS=$delimiter ; echo "${array[*]}")
}

# Sets the patch segment of a semver to 0
# $1: version itself
# shellcheck disable=SC2207,SC2046,SC2248,SC2250
set_semver_patch_to_zero() {
  local delimiter=.
  local array=($(echo "$1" | tr $delimiter '\n'))
  array[2]="0"
  echo $(local IFS=$delimiter ; echo "${array[*]}")
}
