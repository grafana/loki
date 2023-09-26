#!/usr/bin/env bash

if [[ -z "${NFPM_SIGNING_KEY_FILE}" ]]; then
    echo "NFPM_SIGNING_KEY_FILE is not set"
    exit 1
fi
if [[ -z "${NFPM_PASSPHRASE}" ]]; then
    echo "NFPM_PASSPHRASE is not set"
    exit 1
fi

rm -rf dist/tmp && mkdir -p dist/tmp/packages
unzip dist/\*.zip -d dist/tmp/packages

for name in loki loki-canary logcli promtail; do
    for arch in amd64 arm64 arm; do
        config_path="dist/tmp/config-${name}-${arch}.json"
        jsonnet -V "name=${name}" -V "arch=${arch}" "tools/packaging/nfpm.jsonnet" > "${config_path}"
        nfpm package -f "${config_path}" -p rpm -t dist/
        nfpm package -f "${config_path}" -p deb -t dist/
    done
done

rm -rf dist/tmp