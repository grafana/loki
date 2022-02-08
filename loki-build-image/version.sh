#!/usr/bin/env bash

# The version is defined as year-month-data.number of commits.
# This yields a unique version on `main`.
echo "$(date +%F).$(git rev-list --count HEAD)"
