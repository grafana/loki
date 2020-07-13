#!/bin/bash

source ~/.profile

COMMAND="$@"
if [[ -z "$COMMAND" ]]; then COMMAND="rake test"; fi

for VERSION in `rvm list strings | sort`; do
	rvm $VERSION
	echo $(ruby -v)
	RUBYOPT=-rubygems $COMMAND
done
