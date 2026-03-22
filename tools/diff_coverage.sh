#!/bin/bash

if [[ ! -f "$1" ]] || [[ ! -f "$2" ]]; then
    echo "unable to compare test coverage: both old and new files must exist"
    exit 0
fi

echo
echo
echo '**Change in test coverage per package. Green indicates 0 or positive change, red indicates that test coverage for a package fell.**'
echo

echo '```diff'
for pkg in ${3//,/ }; do
  old=$(grep "pkg/${pkg}\s" "$1" | sed s/%// | awk '{print $5}')
  new=$(grep "pkg/${pkg}\s" "$2" | sed s/%// | awk '{print $5}')
  echo | awk -v pkg="${pkg}" -v old="${old:-0}" -v new="${new:-0}" \
  '{
      sign=new - old < 0 ? "-" : "+"
      printf ("%s %18s\t%s%%\n", sign, pkg, new - old)
  }'
done
echo '```'
