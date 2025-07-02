find . -name "*_gen.go" -delete
find . -name "*.wasm" -delete
go generate ./...

for dir in demoplugins/*/; do
    if [ -d "$dir" ]; then
        echo "Building plugin in $dir"
        name=$(basename "$dir")
        tinygo build -buildmode=c-shared -target=wasip1 -o ${dir}/${name}.wasm ${dir}/${name}.go
    fi
done

go test -test.v  .
