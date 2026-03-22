---
title: multiline
menuTitle:  
description: The 'multiline' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/multiline/
weight:  
---

# multiline

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `multiline` stage merges multiple lines into a multiline block before passing it on to the next stage in the pipeline.

A new block is identified by the `firstline` regular expression. Any line that does *not* match the expression is considered to be part of the block of the previous match.

## Schema

```yaml
multiline:
  # RE2 regular expression, if matched will start a new multiline block.
  # This expression must be provided.
  firstline: <string>

  # The maximum wait time will be parsed as a Go duration: https://golang.org/pkg/time/#ParseDuration.
  # If no new logs arrive within this maximum wait time, the current block will be sent on.
  # This is useful if the observed application dies with, for example, an exception.
  # No new logs will arrive and the exception
  # block is sent *after* the maximum wait time expires.
  # It defaults to 3s.
  max_wait_time: <duration>

  # Maximum number of lines a block can have. If the block has more lines, a new block is started.
  # The default is 128 lines.
  max_lines: <integer>
```

## Examples

### Predefined Log Format

Consider these logs from a simple [flask](https://flask.palletsprojects.com) service.

```
[2020-12-03 11:36:20] "GET /hello HTTP/1.1" 200 -
[2020-12-03 11:36:23] ERROR in app: Exception on /error [GET]
Traceback (most recent call last):
  File "/home/pallets/.pyenv/versions/3.8.5/lib/python3.8/site-packages/flask/app.py", line 2447, in wsgi_app
    response = self.full_dispatch_request()
  File "/home/pallets/.pyenv/versions/3.8.5/lib/python3.8/site-packages/flask/app.py", line 1952, in full_dispatch_request
    rv = self.handle_user_exception(e)
  File "/home/pallets/.pyenv/versions/3.8.5/lib/python3.8/site-packages/flask/app.py", line 1821, in handle_user_exception
    reraise(exc_type, exc_value, tb)
  File "/home/pallets/.pyenv/versions/3.8.5/lib/python3.8/site-packages/flask/_compat.py", line 39, in reraise
    raise value
  File "/home/pallets/.pyenv/versions/3.8.5/lib/python3.8/site-packages/flask/app.py", line 1950, in full_dispatch_request
    rv = self.dispatch_request()
  File "/home/pallets/.pyenv/versions/3.8.5/lib/python3.8/site-packages/flask/app.py", line 1936, in dispatch_request
    return self.view_functions[rule.endpoint](**req.view_args)
  File "/home/pallets/src/deployment_tools/hello.py", line 10, in error
    raise Exception("Sorry, this route always breaks")
Exception: Sorry, this route always breaks
[2020-12-03 11:36:23] "GET /error HTTP/1.1" 500 -
[2020-12-03 11:36:26] "GET /hello HTTP/1.1" 200 -
[2020-12-03 11:36:27] "GET /hello HTTP/1.1" 200 -
```

We would like to collapse all lines of the traceback into one multiline block. In this example, all blocks start with a timestamp in brackets. Thus, we configure a `multiline` stage with the `firstline` regular expression `^\[\d{4}-\d{2}-\d{2} \d{1,2}:\d{2}:\d{2}\]`. This will match the start of the traceback, but not the following lines until `Exception: Sorry, this route always breaks`. These will be part of a multiline block and one log entry in Loki.

```yaml
- multiline:
    # Identify timestamps as first line of a multiline block. Enclose the string in single quotes.
    firstline: '^\[\d{4}-\d{2}-\d{2} \d{1,2}:\d{2}:\d{2}\]'
    max_wait_time: 3s
- regex:
    # Flag (?s:.*) needs to be set for regex stage to capture full traceback log in the extracted map.
    expression: '^(?P<time>\[\d{4}-\d{2}-\d{2} \d{1,2}:\d{2}:\d{2}\]) (?P<message>(?s:.*))$'
```

### Custom Log Format

The example assumed you had no control over the log format. Thus, it required a more elaborate regular expression to match the first line. If you can control the log format of the system under observation, we can simplify the first line matching.

This time we are looking at the logs of a simple [Akka HTTP service](https://doc.akka.io/docs/akka-http/current/introduction.html).

```
​​[2021-01-07 14:17:43,494] [DEBUG] [akka.io.TcpListener] [HelloAkkaHttpServer-akka.actor.default-dispatcher-26] [akka://HelloAkkaHttpServer/system/IO-TCP/selectors/$a/0] - New connection accepted
​​[2021-01-07 14:17:43,499] [ERROR] [akka.actor.ActorSystemImpl] [HelloAkkaHttpServer-akka.actor.default-dispatcher-3] [akka.actor.ActorSystemImpl(HelloAkkaHttpServer)] - Error during processing of request: 'oh no! oh is unknown'. Completing with 500 Internal Server Error response. To change default exception handling behavior, provide a custom ExceptionHandler.
java.lang.Exception: oh no! oh is unknown
	at com.grafana.UserRoutes.$anonfun$userRoutes$6(UserRoutes.scala:28)
	at akka.http.scaladsl.server.Directive$.$anonfun$addByNameNullaryApply$2(Directive.scala:166)
	at akka.http.scaladsl.server.ConjunctionMagnet$$anon$2.$anonfun$apply$3(Directive.scala:234)
	at akka.http.scaladsl.server.directives.BasicDirectives.$anonfun$mapRouteResult$2(BasicDirectives.scala:68)
	at akka.http.scaladsl.server.directives.BasicDirectives.$anonfun$textract$2(BasicDirectives.scala:161)
	at akka.http.scaladsl.server.RouteConcatenation$RouteWithConcatenation.$anonfun$$tilde$2(RouteConcatenation.scala:47)
	at akka.http.scaladsl.util.FastFuture$.strictTransform$1(FastFuture.scala:40)
  ...
```

At first sight these seem be like the others. Let's look at the log format.

```xml
<configuration>
    <appender name="FILE" class="ch.qos.logback.core.FileAppender">
        <file>crasher.log</file>
        <append>true</append>
        <encoder>
            <pattern>&ZeroWidthSpace;[%date{ISO8601}] [%level] [%logger] [%thread] [%X{akkaSource}] - %msg%n</pattern>
        </encoder>
    </appender>

    <appender name="ASYNC" class="ch.qos.logback.classic.AsyncAppender">
        <queueSize>1024</queueSize>
        <neverBlock>true</neverBlock>
        <appender-ref ref="STDOUT" />
    </appender>

    <root level="DEBUG">
        <appender-ref ref="ASYNC"/>
    </root>

</configuration>
```

There is nothing special for a [Logback](http://logback.qos.ch/) configuration except for `&ZeroWidthSpace;` at the beginning of each log line. This is the HTML-code for the [Zero-width space](https://en.wikipedia.org/wiki/Zero-width_space) character. It makes identifying first lines much simpler and is not visible. Thus it will not change the view of the log. The new first line matching regular expression is then `\x{200B}\[`. `200B` is the Unicode code point for the zero-width space character.

```yaml
multiline:
  # Identify zero-width space as first line of a multiline block.
  # Note the string should be in single quotes.
  firstline: '^\x{200B}\['

  max_wait_time: 3s
```

Zero-width space might not suite everyone. Any special character that is unlikely to be part of your regular logs should do just fine.
