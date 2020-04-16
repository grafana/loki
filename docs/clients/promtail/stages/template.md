# `template` stage

The `template` stage is a transform stage that lets use manipulate the values in
the extracted map using [Go's template
syntax](https://golang.org/pkg/text/template/).

The `template` stage is primarily useful for manipulating data from other stages
before setting them as labels, such as to replace spaces with underscores or
converting an uppercase string into a lowercase one. `template` can also be used 
to construct messages with multiple keys.

The template stage can also create new keys in the extracted map. 

## Schema

```yaml
template:
  # Name from extracted data to parse. If key in extract data doesn't exist, an
  # entry for it will be created.
  source: <string>

  # Go template string to use. In additional to normal template
  # functions, ToLower, ToUpper, Replace, Trim, TrimLeft, TrimRight,
  # TrimPrefix, TrimSuffix, and TrimSpace are available as functions.
  template: <string>
```

## Examples

```yaml
- template:
    source: new_key
    template: 'hello world!'
```

Assuming no data has been added to the extracted map yet, this stage will first
add `new_key` with a blank value into the extracted map. Then its value will be
set to `hello world!`.

```yaml
- template:
    source: app
    template: '{{ .Value }}_some_suffix'
```

This pipeline takes the value of the `app` key in the existing extracted map and
appends `_some_suffix` to its value. For example, if the extracted map had a
key of `app` and a value of `loki`, this stage would modify the value from
`loki` to `loki_some_suffix`.

```yaml
- template:
    source: app
    template: '{{ ToLower .Value }}'
```

This pipeline takes the current value of `app` from the extracted map and
converts its value to be all lowercase. For example, if the extracted map
contained `app` with a value of `LOKI`, this pipeline would change its value to
`loki`.

```yaml
- template:
    source: output_msg
    template: '{{ .level }} for app {{ ToUpper .app }}'
```

This pipeline takes the current value of `level` and `app` from the extracted map and
a new key `output_msg` will be added to extracted map with evaluated template. 

For example, if the extracted map contained `app` with a value of `loki`, this pipeline would change its value to `LOKI`. Assuming value of `level` is `warn`. A new key `output_msg` will be added to extracted map with value `warn for app LOKI`. 

Any previously extracted keys can be used in `template`. All extracted keys are available for `template` to expand. 

```yaml
- template:
    source: app
    template: '{{ .level }} for app {{ ToUpper .Value }} in module {{.module}}'
```

This pipeline takes the current value of `level`, `app` and `module` from the extracted map and
converts value of `app` to the evaluated template. 

For example, if the extracted map contained `app` with a value of `loki`, this pipeline would change its value to `LOKI`. Assuming value of `level` is `warn` and value of `module` is `test`. Pipeline will change the value of `app` to `warn for app LOKI in module test`. 

Any previously extracted keys can be used in `template`. All extracted keys are available for `template` to expand. Also, if source is available it can be referred as `.Value` in `template`. Here, `app` is provided as `source`. So, it can be referred as `.Value` in `template`. 

```yaml
- template:
    source: app
    template: '{{ Replace .Value "loki" "blokey" 1 }}'
```

The template here uses Go's [`string.Replace`
function](https://golang.org/pkg/strings/#Replace). When the template executes,
the entire contents of the `app` key from the extracted map will have at most
`1` instance of `loki` changed to `blokey`.
