#!/usr/bin/env bash
export TAG=$(tools/image-tag)
envsubst < tools/release-note.md
