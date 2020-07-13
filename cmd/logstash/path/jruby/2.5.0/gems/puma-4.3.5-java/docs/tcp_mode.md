# TCP mode

Puma also could be used as a TCP server to process incoming TCP
connections.


## Configuration

TCP mode can be enabled with CLI option `--tcp-mode`:

```
$ puma --tcp-mode
```

Default ip and port to listen to are `0.0.0.0` and `9292`. You can configure
them with `--port` and `--bind` options:

```
$ puma --tcp-mode --bind tcp://127.0.0.1:9293
$ puma --tcp-mode --port 9293
```

TCP mode could be set with a configuration file as well with `tcp_mode`
and `tcp_mode!` methods:

```
# config/puma.rb
tcp_mode
```

When Puma starts in the TCP mode it prints the corresponding message:

```
puma --tcp-mode
Puma starting in single mode...
...
* Mode: Lopez Express (tcp)
```


## How to declare an application

An application to process TCP connections should be declared as a
callable object which accepts `env` and `socket` arguments.

`env` argument is a Hash with following structure:

```ruby
{ "thread" => {}, "REMOTE_ADDR" => "127.0.0.1:51133", "log" => "#<Proc:0x000..." }
```

It consists of:
* `thread` - a Hash for each thread in the thread pool that could be
  used to store information between requests
* `REMOTE_ADDR` - a client ip address
* `log` - a proc object to write something down

`log` object could be used this way:

```ruby
env['log'].call('message to log')
#> 19/Oct/2019 20:28:53 - 127.0.0.1:51266 - message to log
```


## Example of an application

Let's look at an example of a simple application which just echoes
incoming string:

```ruby
# config/puma.rb
app do |env, socket|
  s = socket.gets
  socket.puts "Echo #{s}"
end
```

We can easily access the TCP server with `telnet` command and receive an
echo:

```shell
telnet 0.0.0.0 9293
Trying 0.0.0.0...
Connected to 0.0.0.0.
Escape character is '^]'.
sssss
Echo sssss
^CConnection closed by foreign host.
```


## Socket management

After the application finishes, Puma closes the socket. In order to
prevent this, the application should set `env['detach'] = true`.
