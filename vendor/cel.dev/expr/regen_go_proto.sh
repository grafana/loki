#!/bin/sh
bazel build //proto/test/...
files=($(bazel aquery 'kind(proto, //proto/...)' | grep Outputs | grep "[.]pb[.]go" | sed 's/Outputs: \[//' | sed 's/\]//' | tr "," "\n"))
for src in ${files[@]};
do
  dst=$(echo $src | sed 's/\(.*\%\/github.com\/google\/cel-spec\/\(.*\)\)/\2/')
  echo "copying $dst"
  $(cp $src $dst)
done
