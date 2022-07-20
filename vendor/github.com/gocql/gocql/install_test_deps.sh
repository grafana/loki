#!/usr/bin/env bash

set -x

# This is not supposed to be an error-prone script; just a convenience.

# Install CCM
pip install -i https://pypi.org/simple --user cql PyYAML six psutil
git clone https://github.com/pcmanus/ccm.git
pushd ccm
./setup.py install --user
popd
