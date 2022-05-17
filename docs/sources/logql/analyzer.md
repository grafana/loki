---
title: LoqQL Analyzer
weight: 60
---

<link rel="stylesheet" href="../analyzer/style.css">
<script src="../analyzer/handlebars.js"></script>

# LogQL Analyzer

<main class="logql-analyzer">
    <section class="logs-source panel-container">
        <div class="logs-source__header">
            <div class="examples">
                <span>Examples:</span> 
                <span class="example">
                    <input type="radio" class="example-select" name="example" id="logfmt-example" checked>
                    <label for="logfmt-example">Logfmt</label>
                </span>
                <span class="example">
                    <input type="radio" class="example-select" name="example" id="json-parser-example">
                    <label for="json-parser-example">JSON Parser</label>
                </span>
                <span class="example">
                    <input type="radio" class="example-select" name="example" id="pattern-parser-example">
                    <label for="pattern-parser-example">Pattern parser</label>
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
                    <i class="line-cursor expand-cursor"></i>
                    Line {{@index}}
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
                        Origin log line
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
                            <span>stage #{{@index}}:</span>
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
</script>

<script type="text/plain" id="json-parser-example-query">
| json | level="INFO" | line_format "{{.message}}"
</script>


[//]: # (Pattern parser examples)
<script type="text/plain" id="pattern-parser-example-logs">
192.0.2.0 - - [04/Aug/2021:21:12:04 +0000] "GET /api/plugins/versioncheck?slugIn=&grafanaVersion=6.3.5 HTTP/1.1" 200 2 "-" "Go-http-client/2.0" "220.248.51.226, 34.120.177.193" "TLSv1.2" "CN" "CN31"
198.51.100.0 - - [04/Aug/2021:21:12:04 +0000] "GET /ws/?EIO=3&transport=polling&t=NiJ0b8H HTTP/1.1" 200 103 "https://grafana.com/grafana/download?platform=mac" "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.1.1 Safari/605.1.15" "2001:240:168:3400::1:87, 2600:1901:0:b3ea::" "TLSv1.3" "JP" "JP13"
203.0.113.0 - - [04/Aug/2021:21:12:04 +0000] "GET /healthz HTTP/1.1" 500 15 "-" "GoogleHC/1.0" "-" "-" "-" "-"
</script>

<script type="text/plain" id="pattern-parser-example-query">
| pattern "<_> - - <_> \"<method> <url> <protocol>\" <status> <_> <_> \"<_>\" <_>" | status = "200"
</script>


<script src="../analyzer/script.js"> </script>


