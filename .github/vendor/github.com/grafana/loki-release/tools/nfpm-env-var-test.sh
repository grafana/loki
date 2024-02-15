#!/usr/bin/env bash

if [[ -z "${NFPM_SIGNING_KEY_FILE}" ]]; then
    echo "NFPM_SIGNING_KEY_FILE is not set"
    exit 1
fi

if [[ -z "${NFPM_PASSPHRASE}" ]]; then
    echo "NFPM_PASSPHRASE is not set"
    exit 1
fi
