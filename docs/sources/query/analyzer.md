---
title: Simple LogQL simulator
menuTitle: LogQL simulator
description: The LogQL simulator is an online educational tool for experimenting with writing simple LogQL queries.
aliases:
- ../logql/analyzer/
weight: 200
---


<link rel="stylesheet" href="../analyzer/style.css">
<script src="../analyzer/handlebars.js"></script>

# Simple LogQL simulator

The LogQL simulator is an online tool that you can use to experiment with writing simple LogQL queries and seeing the results, without needing to run an instance of Loki.

A set of example log lines are included for each of the primary log parsers supported by Loki:

- [Logfmt](https://brandur.org/logfmt)
- [JSON](https://www.json.org/json-en.html)
- Unstructured text, which can be parsed with the Loki pattern or regex parsers

The [log stream selector](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#log-stream-selector) `{job="analyze"}` is shown as an example, and it remains fixed for all possible example queries in the simulator. A log stream is a set of logs which share the same labels. In LogQL, you use a log stream selector to determine which log streams to include in a query's results.

{{< admonition type="note" >}}
This is a very limited simulator, primarily for evaluating filters and parsers. If you want to practice writing more complex queries, such as metric queries, you can use the [Explore](https://grafana.com/docs/grafana/<GRAFANA_VERSION>/explore/logs-integration/) feature in Grafana.
{{< /admonition >}}

To use the LogQL simulator:

1. Select a log line format using the radio buttons.

1. You can use the provided example log lines, or copy and paste your own log lines into the example log lines box.

1. Use the provided example LogQL query, or enter your own query. The [log stream selector](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#log-stream-selector) remains fixed for all possible example queries. There are additional sample queries at the end of this topic.

1. Click the **Run query** button to run the entered query against the example log lines.

The results output simulates how Loki would return results for your query. You can also click each line in the results pane to expand the details, which give an explanation for why the log line is or is not included in the query result set.

<main class="logql-analyzer">
    <section class="logs-source panel-container">
        <div class="logs-source__header">
            <div class="examples">
                <span>Log line format:</span>
                <span class="example">
                    <input type="radio" class="example-select" name="example" id="logfmt-example" checked>
                    <label for="logfmt-example">logfmt</label>
                </span>
                <span class="example">
                    <input type="radio" class="example-select" name="example" id="json-parser-example">
                    <label for="json-parser-example">JSON</label>
                </span>
                <span class="example">
                    <input type="radio" class="example-select" name="example" id="pattern-parser-example">
                    <label for="pattern-parser-example">Unstructured text</label>
                </span>
            </div>
            <div class="share-section">
                <span class="share-link-copied-notification hide" id="share-link-copied-notification">
                    <i class="fa fa-check" aria-hidden="true"></i>
                    Link copied to clipboard.
                </span>
                <button class="primary-button" id="share-button">
                    <i class="fa fa-link" aria-hidden="true"></i>
                    Share
                </button>
            </div>
        </div>
        <div class="panel-header">
            {job="analyze"}
        </div>
        <textarea id="logs-source-input" class="logs-source__input"></textarea>
    </section>
    <section class="query panel-container">
        <div class="panel-header">
            Query:
        </div>
        <div class="query-container">
            <div class="input-box">
                <span class="prefix">{job="analyze"} </span>
                <input id="query-input" class="query_input">
            </div>
            <button class="query_submit primary-button">Run query</button>
        </div>
        <div class="query-error" id="query-error"></div>
    </section>
    <section class="results panel-container hide" id="results">
    </section>

</main>

<script id="log-result-template" type="text/x-handlebars-template">
    <div class="panel-header">
        Results
    </div>
    {{#each results}}
        <article class="debug-result-row">
            <div class="last-stage-result" data-line-index="{{@index}}">
                <div class="line-index">
                    <div class="line-index__wrapper">
                        <i class="line-cursor expand-cursor"></i>
                        <span>Line {{inc @index}}</span>
                    </div>
                </div>
                {{#if this.log_result}}
                    <span {{#if this.filtered_out}}class="filtered-out"{{/if}}>
                        {{this.log_result}}
                    </span>
                {{/if}}
                {{#unless this.log_result}}
                    <span class="note-text">(empty line)</span>
                {{/unless}}
            </div>

            <div class="debug-result-row__explain hide">
                <div class="explain-section origin-line">
                    <div class="explain-section__header">
                        Original log line
                        <span class="stage-expression">{{../stream_selector}}</span>
                    </div>
                    <div class="explain-section__body">
                        {{this.origin_line}}
                        {{#unless this.log_result}}
                            <span class="note-text">(empty line)</span>
                        {{/unless}}
                    </div>
                </div>
                {{#each this.stages}}
                    <div class="arrow-wrapper">
                        <i class="fa fa-arrow-down" aria-hidden="true"></i>
                    </div>
                    <div class="explain-section stage-line">
                        <div class="explain-section__header">
                            <span>stage #{{inc @index}}:</span>
                            <span class="stage-expression"> {{stage_expression}} </span>
                        </div>
                        <div class="explain-section__body">
                            <div class="explain-section__row">
                                <div class="explain-section__row-title">
                                    Available labels on this stage:
                                </div>
                                <div class="explain-section__row-body">
                                    {{#unless labels_before}}
                                        <span>none</span>
                                    {{/unless}}
                                    {{#if labels_before}}
                                        {{#each labels_before}}
                                            <article class="label-value" style="background-color: {{background_color}}">
                                                {{name}}={{value}}
                                            </article>
                                        {{/each}}
                                    {{/if}}
                                </div>
                            </div>
                            <div class="explain-section__row">
                                <div class="explain-section__row-title">
                                    Line after this stage:
                                </div>
                                <div class="explain-section__row-body">
                                    {{#if line_after}}
                                        <span {{#if this.filtered_out}}class="filtered-out"{{/if}}>
                                            {{line_after}}
                                        </span>
                                    {{/if}}
                                    {{#unless line_after}}
                                        <span class="note-text">(empty line)</span>
                                    {{/unless}}
                                    {{#if this.filtered_out}}
                                        <span class="important-text">the line has been filtered out on this stage</span>
                                    {{/if}}
                                </div>
                            </div>
                            {{#if added_labels}}
                                <div class="explain-section__row">
                                    <div class="explain-section__row-title">
                                        Added/Modified labels:
                                    </div>
                                    <div class="explain-section__row-body">
                                        {{#each added_labels}}
                                            <article class="label-value"  style="background-color: {{background_color}}">
                                                {{name}}={{value}}
                                            </article>
                                        {{/each}}
                                    </div>
                                </div>
                            {{/if}}
                        </div>
                    </div>
                {{/each}}
            </div>
        </article>
    {{/each}}
</script>

[//]: # (Logfmt examples)
<script type="text/plain" id="logfmt-example-logs">
level=info ts=2022-03-23T11:55:29.846163306Z caller=main.go:112 msg="Starting Grafana Enterprise Logs"
level=debug ts=2022-03-23T11:55:29.846226372Z caller=main.go:113 version=v1.3.0 branch=HEAD Revision=e071a811 LokiVersion=v2.4.2 LokiRevision=525040a3
level=warn ts=2022-03-23T11:55:45.213901602Z caller=added_modules.go:198 msg="found valid license" cluster=enterprise-logs-test-fixture
level=info ts=2022-03-23T11:55:45.214611239Z caller=server.go:269 http=[::]:3100 grpc=[::]:9095 msg="server listening on addresses"
level=debug ts=2022-03-23T11:55:45.219665469Z caller=module_service.go:64 msg=initialising module=license
level=warm ts=2022-03-23T11:55:45.219678992Z caller=module_service.go:64 msg=initialising module=server
level=error ts=2022-03-23T11:55:45.221140583Z caller=manager.go:132 msg="license manager up and running"
level=info ts=2022-03-23T11:55:45.221254326Z caller=loki.go:355 msg="Loki started"
</script>

<script type="text/plain" id="logfmt-example-query">
| logfmt | level = "info"
</script>

[//]: # (Json parser examples)
<script type="text/plain" id="json-parser-example-logs">
{"timestamp":"2022-04-26T08:53:59.61Z","level":"INFO","class":"org.springframework.boot.SpringApplication","method":"logStartupProfileInfo","file":"SpringApplication.java","line":663,"thread":"restartedMain","message":"The following profiles are active: no-schedulers,json-logging"}
{"timestamp":"2022-04-26T08:53:59.645Z","level":"DEBUG","class":"org.springframework.boot.logging.DeferredLog","method":"logTo","file":"DeferredLog.java","line":255,"thread":"restartedMain","message":"Devtools property defaults active! Set 'spring.devtools.add-properties' to 'false' to disable"}
{"timestamp":"2022-04-26T08:53:59.645Z","level":"DEBUG","class":"org.springframework.boot.logging.DeferredLog","method":"logTo","file":"DeferredLog.java","line":255,"thread":"restartedMain","message":"For additional web related logging consider setting the 'logging.level.web' property to 'DEBUG'"}
{"timestamp":"2022-04-26T08:54:00.274Z","level":"INFO","class":"org.springframework.data.repository.config.RepositoryConfigurationDelegate","method":"registerRepositoriesIn","file":"RepositoryConfigurationDelegate.java","line":132,"thread":"restartedMain","message":"Bootstrapping Spring Data JPA repositories in DEFAULT mode."}
{"timestamp":"2022-04-26T08:54:00.327Z","level":"INFO","class":"org.springframework.data.repository.config.RepositoryConfigurationDelegate","method":"registerRepositoriesIn","file":"RepositoryConfigurationDelegate.java","line":201,"thread":"restartedMain","message":"Finished Spring Data repository scanning in 47 ms. Found 3 JPA repository interfaces."}
{"timestamp":"2022-04-26T08:54:00.704Z","level":"INFO","class":"org.springframework.boot.web.embedded.tomcat.TomcatWebServer","method":"initialize","file":"TomcatWebServer.java","line":108,"thread":"restartedMain","message":"Tomcat initialized with port(s): 8080 (http)"}
{"timestamp":"2022-06-16T10:54:47.466Z","level":"INFO","class":"org.apache.juli.logging.DirectJDKLog","method":"log","file":"DirectJDKLog.java","line":173,"thread":"restartedMain","message":"Starting service [Tomcat]"}
{"timestamp":"2022-06-16T10:54:47.467Z","level":"INFO","class":"org.apache.juli.logging.DirectJDKLog","method":"log","file":"DirectJDKLog.java","line":173,"thread":"restartedMain","message":"Starting Servlet engine: [Apache Tomcat/9.0.52]"}
</script>

<script type="text/plain" id="json-parser-example-query">
| json | level="INFO" | line_format "{{.message}}"
</script>

[//]: # (Pattern parser examples)
<script type="text/plain" id="pattern-parser-example-logs">
238.46.18.83 - - [09/Jun/2022:14:13:44 -0700] "PUT /target/next-generation HTTP/2.0" 404 19042
16.97.233.22 - - [09/Jun/2022:14:13:44 -0700] "DELETE /extensible/functionalities HTTP/1.0" 200 27913
46.201.144.32 - - [09/Jun/2022:14:13:44 -0700] "PUT /e-enable/enable HTTP/2.0" 504 26885
33.122.3.191 - corkery3759 [09/Jun/2022:14:13:44 -0700] "POST /extensible/dynamic/enable HTTP/2.0" 100 23741
94.115.144.32 - damore5842 [09/Jun/2022:14:13:44 -0700] "PUT /matrix/envisioneer HTTP/1.0" 205 29993
145.250.221.107 - price8727 [09/Jun/2022:14:13:44 -0700] "PUT /iterate/networks/e-business/action-items HTTP/1.0" 302 9718
33.201.165.66 - - [09/Jun/2022:14:13:44 -0700] "GET /web-enabled/bricks-and-clicks HTTP/1.0" 205 2353
33.83.191.176 - kling8903 [09/Jun/2022:14:13:44 -0700] "DELETE /architect HTTP/1.1" 401 13783
</script>

<script type="text/plain" id="pattern-parser-example-query">
| pattern "<_> - <_> <_> \"<method> <url> <protocol>\" <status> <_> <_> \"<_>\" <_>" | status >= 200 and status < 300
</script>


<script src="../analyzer/script.js"> </script>

## Additional sample queries

These are some additional sample queries that you can use in the LogQL simulator.

### Logfmt

```logql
| logfmt | level = "debug"
```

Parses logfmt-formatted logs and returns only log lines where the "level" field is equal to "debug".

```logql
| logfmt | msg="server listening on addresses"
```

Parses logfmt-formatted logs and returns only log lines with the message “server listening on address.”

### JSON

```logql
| json | level="INFO" | file="SpringApplication.java" | line_format `{{.class}}`
```

Parses JSON-formatted logs, filtering for lines where the 'level' field is "INFO" and the 'file field is "SpringApplication.java", then formats the line to return only the 'class' field.

```logql
|~ `(T|t)omcat`
```

Performs a regular expression filter for the string 'tomcat' or 'Tomcat', without using a parser.

### Unstructured text

```logql
| pattern "<_> - <_> <_> \"<method> <url> <protocol>\" <status> <_> <_> \"<_>\" <_>" | method="GET"
```

Parses unstructured logs with the pattern parser, filtering for lines where the HTTP method is "GET".

```logql
| pattern "<_> - <user> <_> \"<method> <url> <protocol>\" <status> <_> <_> \"<_>\" <_>" | user=~"kling.*"
```

Parses unstructured logs with the pattern parser, extracting the 'user' field, and filtering for lines where the user field starts with "kling".
