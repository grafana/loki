#!/bin/sh

ruby ./benchmarking/individual/json-mri-sourced.rb
ruby ./benchmarking/individual/oj-mri-sourced.rb

ruby ./benchmarking/individual/json-gen-mri-sourced.rb
ruby ./benchmarking/individual/oj-gen-mri-sourced.rb
