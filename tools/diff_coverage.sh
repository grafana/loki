#!/bin/bash

if [[ ! -f "$1" ]] || [[ ! -f "$2" ]]; then
    echo "unable to compare test coverage: both old and new files must exist"
    exit 0
fi

echo '```diff'
for pkg in ingester distributor querier ruler storage; do
  old=$(grep "pkg/${pkg}\s" "$1" | sed s/%// | awk '{print $5}')
  new=$(grep "pkg/${pkg}\s" "$2" | sed s/%// | awk '{print $5}')
  echo | awk -v pkg="${pkg}" -v old="${old:-0}" -v new="${new:-0}" \
  '{
      sign=new - old > 0 ? "+" : "-"
      printf ("%s %11s\t%s\n", sign, pkg, new - old)
  }'
done
echo '```'
