# Labels from Logs
Author: Ed Welch  
Date: February 2019

This is the official version of this doc as of 2019/04/03, the original discussion was had via a [Google doc](https://docs.google.com/document/d/16y_XFux4h2oQkJdfQgMjqu3PUxMBAq71FoKC_SkHzvk/edit?usp=sharing), which is being kept for posterity but will not be updated moving forward.

## Problem Statement

We should be able to filter logs by labels extracted from log content.

Keeping in mind:
Loki is not a log search tool and we need to discourage the use of log labels as an attempt to recreate log search functionality.  Having a label on “order number” would be bad, however, having a label on “orderType=plant” and then filtering the results on a time window with an order number would be fine.  (think: grep “plant” | grep “12324134” )
Loki as a grep replacement, log tailing or log scrolling tool is highly desirable, log labels will be useful in reducing query results and improving query performance, combined with logQL to narrow down results. 

## Use Cases

As defined for prometheus “Use labels to differentiate the characteristics of the thing that is being measured” There are common cases where someone would want to search for all logs which had a level of “Error” or for a certain HTTP path (possibly too high cardinality), or of a certain order or event type.
Examples:
* Log levels.
* HTTP Status codes.
* Event type.

## Challenges

* Logs are often unstructured data, it can be very difficult to extract reliable data from some unstructured formats, often requiring the use of complicated regular expressions. 
* Easy to abuse.  Easy to create a Label with high cardinality, even possibly by accident with a rogue regular expression.
* Where do we extract metrics and labels at the client (Promtail or other?) or Loki? Extraction at the server (Loki) side has some pros/cons.  Can we do both? At least with labels we could define a set of expected labels and if loki doesn’t receive them they could be extracted.
  * Server side extraction would improve interoperability at the expense of increase server workload and cost.
  * Are there discoverability questions/concerns with metrics exposed via loki vs the agent? Maybe this is better/easier to manage?
  * Potentially more difficult to manage configuration with the server side having to match configs to incoming log streams

## Existing Solutions

There already exist solutions for extracting processing and extracting metrics from unstructured log data, however, they will not quite work for extracting labels without some work and neither support easy inclusion as a library.  It’s worth noting and understanding how they work to try to get the best features in our solution.
mtail
https://github.com/google/mtail
1721 github stars, huge number of commits, releases and contributors, google project

All go, uses go RE2 regular expressions which is going to be more performant than grok_exporter below which uses a full regex implementation allowing backtracking and lookahead required to be compliant with Grok but which are also slower.

grok_exporter
https://github.com/fstab/grok_exporter
278 github stars, mature/active project

If you are familiar with Grok this would be more comfortable, many people use ELK stacks and would likely be familiar with or already have Grok strings for their logs, making it easy to use grok_exporter to extract metrics.

One caveat is the dependency on the oniguruma C library which parses the regular expressions. 

## Implementation

### Details

As mentioned previously in the challenges for working with unstructured data, there isn’t a good one size fits all solution for extracting structured data.  

The Docker log format is an example where multiple levels of processing may be required, where the docker log is json, however, it also contains the log message field which itself could be embedded json, or a log message which needs regex parsing.

A pipelined approach should allow for handling these more challenging scenarios

There are 2 interfaces within Promtail already that should support constructing a pipeline:

```go
type EntryMiddleware interface {
    Wrap(next EntryHandler) EntryHandler
}
```

```go
type EntryHandler interface {
    Handle(labels model.LabelSet, time time.Time, entry string) error
}
````

Essentially every entry in the pipeline will Wrap the log line with another EntryHandler which can add to the LabelSet, set the timestamp, and mutate(or not) the log line) before it gets handed to the next stage in the pipeline.

### Example

```json
{
  "log": "level=info msg=\”some log message\”\n",
  "stream": "stderr",
  "time": "2012-11-01T22:08:41+00:00"
}
```

This is a docker format log file which is JSON but also contains a log message which has some key-value pairs.

Our pipelined config might look like this:

```yaml
scrape_configs:
- job_name: system
  entry_parsers:
    - json: 
        timestamp:
          source: time
          format: RFC3339
        labels:
          stream:
            source: json_key_name.json_sub_key_name
        output: log
    - regex:
        expr: '.*level=(?P<level>[a-zA-Z]+).*'
        labels:
          level:
    - regex:
        expr: '.*msg=(?P<message>[a-zA-Z]+).*'
        output: message
```

Looking at this a little closer:

```yaml
     - json: 
        timestamp:      
          source: time  
          format: TODO                               ①
        labels:
          stream:                         
            source: json_key_name.json_sub_key_name  ②
        output: log                                  ③
```


① The format key will likely be a format string for Go’s time.Parse or a format string for strptime, this still needs to be decided, but the idea would be to specify a format string used to extract the timestamp data, for the regex parser there would also need to be a expr key used to extract the timestamp.  
② One of the json elements was “stream” so we extract that as a label, if the json value matches the desired label name it should only be required to specify the label name as a key, if some mapping is required you can optionally provide a “source” key to specify where to find the label in the document. (Note the use of `json_key_name.json_sub_key_name` is just an example here and doesn't match our example log)  
③ Tell the pipeline which element from the json to send to the next stage.  

```yaml
    - regex:
        expr: '.*level=(?P<level>[a-zA-Z]+).*'  ①
        labels:
          level:                                ②
```

① Define the Go RE2 regex, making sure to use a named capture group.  
② Extract labels using the named capture group names.  

Notice there was not an output section defined here, omitting the output key should instruct the parser to return the incoming log message to the next stage with no changes.

```yaml
    - regex:
        expr: '.*msg=(?P<message>[a-zA-Z]+).*'
        output: message                          ①
```

① Send the log message as the output to the last stage in the pipeline, this will be what you want Loki to store as the log message.

There is an alternative configuration that could be used here to accomplish the same result:

```yaml
scrape_configs:
- job_name: system
  entry_parsers:
    - json: 
        timestamp:
          source: time
          format: FIXME
        labels:
          stream:
        output: log
    - regex:
        expr: '.*level=(?P<level>[a-zA-Z]+).*msg=(?P<message>[a-zA-Z]+).*'
        labels:
          level:                                                             ①
          log:
            source: message                                                  ②
        output: message
```

① Similar to the json parser, if your log label matches the regex named group, you need only specify the label name as a yaml key  
② If you had a use case for specifying a different label name from the regex group name you can optionally provide the `source` key with the value matching the named capture group.

You can define a more complicated regular expression with multiple capture groups to extract many labels and/or the output log message in one entry parser.  This has the advantage of being more performant, however, the regular expression will also get much more complicated.

Please also note the regex for `message` is incomplete and would do a terrible job of matching any standard log message which might contain spaces or non alpha characters.

### Concerns
* Debugging, especially if a pipeline stage is mutating the log entry. 
* Clashing labels and how to handle this (two stages try to set the same label) 
* Performance vs ease of writing/use, if every label is extracted one at a time and there are a lot of labels and a long line, it would force reading the line many times, however contrast this to a really long complicated regex which only has to read the line once but is difficult to write and/or change and maintain

### Further improvements
There are some basic building blocks for our pipeline which will use the EntryMiddleware interface, the two most commonly used will likely be:

* Regex Parser
* JSON Parser

However we don’t want to ask people to copy and paste basic configs over and over for very common use cases, so it would make sense to add some additional parsers which would really be supersets of the base parsers above.

For example, the config above might be simplified to:

```yaml
scrape_configs:
- job_name: system
  entry_parsers:
    - docker:
```

or

```yaml
scrape_configs:
- job_name: system
  entry_parsers:
    - cri:
```

Which could still easily be extended to extract additional labels:

```yaml
scrape_configs:
- job_name: system
  entry_parsers:
    - docker:
    - regex:
        expr: '.*level=(?P<level>[a-zA-Z]+).*'
        labels:
          level:
```

### Auto Detection?
An even further simplification would be to attempt to autodetect the log format, a PR for this work has been submitted, then the config could be as simple as:

```yaml
scrape_configs:
- job_name: system
  entry_parsers:
    - auto:
```

This certainly has some advantages for people first adopting and testing Loki, allowing them to point it at their logs and at least get the timestamp and log message extracted properly for the common formats like Docker and CRI.

There are also some challenges with auto detection and edge cases, though most people are going to want to augment the basic config with additional labels, so maybe it makes sense to default to auto but suggest when people start writing configs they chose the correct parser?

## Other Thoughts and Considerations

* We should have a standalone client in some fashion which allows for testing of log parsing at the command line, allowing users to validate regular expressions or configurations to see what information is extracted.
* Other input formats to the pipeline which are not reading from log files, such as containerd grpc api, or from stdin or unix pipes, etc.
* It would be nice if at some point we could support loading code into the pipeline stages for even more advanced/powerful parsing capabilities.
