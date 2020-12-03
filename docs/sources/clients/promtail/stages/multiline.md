---
title: multiline 
---

# `multiline` stage

The `multiline` stage multiple lines into a multiline block before passing it on to the next stage in the pipeline.

A new block is identified by the `firstline` regular expression. Any line that does *not* match the expression is considered to be part of the block of the previous match.

## Schema

```yaml
multiline:
  # RE2 regular expression, if matched will start a new multiline block.
  # This expresion must be provided.
  firstline: <string>

  # The maximum wait time will be parsed as a Go duration: https://golang.org/pkg/time/#ParseDuration.
  # If now new logs arrive withing this maximum wait time the current block will be sent on.
  # This is useful if the opserved application dies with e.g. an exception. No new logs will arrive and the exception
  # block is sent *after* the maximum wait time expired.
  max_wait_time: <duration>

  # Maximum number of lines a block can have. If block has more lines a new block is started.
  # The default is 128 lines.
  max_lines: <integer>
```

## Examples

Let's say we have the following logs from a very simple [flask](https://flask.palletsprojects.com) service.

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

We would like to collapse all lines of the traceback into one multiline block. All blocks start with a timestamp in brackets. Thus we configure a `multiline` stage with the `firstline` regular expression `^\[\d{4}-\d{2}-\d{2} \d{1,2}:\d{2}:\d{2}\]`. This will match the start of the traceback but not the following lines until `Exception: Sorry, this route always breaks`. These will be part of a multiline block and one log entry in Loki.

```yaml
multiline:
  # Identify timestamps as first line of a multiline block.
  firstline: "^\[\d{4}-\d{2}-\d{2} \d{1,2}:\d{2}:\d{2}\]"

  max_wait_time: 3s
```