#!/bin/bash
# Copyright 2019 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script generates all GAPIC clients in this repo.
# See instructions at go/yoshi-site.

set -ex

GOCLOUD_DIR="$(dirname "$0")"
HOST_MOUNT="$PWD"

# need to mount the /var/folders properly for macos
# https://stackoverflow.com/questions/45122459/docker-mounts-denied-the-paths-are-not-shared-from-os-x-and-are-not-known/45123074
if [[ "$OSTYPE" == "darwin"* ]] && [[ "$HOST_MOUNT" == "/var/folders"* ]]; then
  HOST_MOUNT=/private$HOST_MOUNT
fi

microgen() {
  input=$1
  options="${@:2}"

  # see https://github.com/googleapis/gapic-generator-go/blob/master/README.md#docker-wrapper for details
  docker run \
    --user $UID \
    --mount type=bind,source=$HOST_MOUNT,destination=/conf,readonly \
    --mount type=bind,source=$HOST_MOUNT/$input,destination=/in/$input,readonly \
    --mount type=bind,source=/tmp,destination=/out \
    --rm \
    gcr.io/gapic-images/gapic-generator-go:0.9.1 \
    $options
}

for gencfg in $(cat $GOCLOUD_DIR/gapics.txt); do
  rm -rf artman-genfiles/*
  artman --config "$gencfg" generate go_gapic
  cp -r artman-genfiles/gapi-*/cloud.google.com/go/* $GOCLOUD_DIR
done

rm -rf /tmp/cloud.google.com
{
  # skip the first line with column titles
  read -r
  while IFS=, read -r input mod retrycfg apicfg release
  do
    microgen $input "$mod" "$retrycfg" "$apicfg" "$release"
  done
} < $GOCLOUD_DIR/microgens.csv

# copy generated code if any was created
[ -d "/tmp/cloud.google.com/go" ] && cp -r /tmp/cloud.google.com/go/* $GOCLOUD_DIR

pushd $GOCLOUD_DIR
  gofmt -s -d -l -w . && goimports -w .

  # NOTE(pongad): `sed -i` doesn't work on Macs, because -i option needs an argument.
  # `-i ''` doesn't work on GNU, since the empty string is treated as a file name.
  # So we just create the backup and delete it after.
  ver=$(date +%Y%m%d)
  git ls-files -mo | while read modified; do
    dir=${modified%/*.*}
    find . -path "*/$dir/doc.go" -exec sed -i.backup -e "s/^const versionClient.*/const versionClient = \"$ver\"/" '{}' +
  done
popd

for manualdir in $(cat $GOCLOUD_DIR/manuals.txt); do
  find "$GOCLOUD_DIR/$manualdir" -name '*.go' -exec sed -i.backup -e 's/setGoogleClientInfo/SetGoogleClientInfo/g' '{}' '+'
done

find $GOCLOUD_DIR -name '*.backup' -delete
