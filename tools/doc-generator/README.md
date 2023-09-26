# Documentation generation tool

The doc-generator tool is used to automatically generate the configuration flags documentation from the metadata information
provided in the code. 

## Run 

The tool receives as input a template file and generates the list of configuration value blocks: 

```shell
go run ./tools/doc-generator docs/sources/configuration/index.template > docs/sources/configuration/_index.md
```

## `doc` tag

The description and default value of configuration values can be set via CLI flag registration by using the `flag` package. 
However, for a more flexible documentation generation it is possible to combine this with the `doc` tag by applying the following custom values:

* `doc:"deprecated"`: sets the element as deprecated in the documentation.
* `doc:"hidden"`: does not show the element in the documentation.
* `doc:"description=foo"`: overrides the element's description (set via flag registration, if any) with `foo`.
* `doc:"default=<hostname>"`: sets the element's documentation default value as `<hostname>`. 
Note: this only sets the default value shown in the documentation, it doesn't override the default configuration value. 