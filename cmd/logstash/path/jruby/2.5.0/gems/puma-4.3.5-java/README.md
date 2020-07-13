<p align="center">
  <img src="https://puma.io/images/logos/puma-logo-large.png">
</p>

# Puma: A Ruby Web Server Built For Concurrency

[![Gitter](https://badges.gitter.im/Join%20Chat.svg)](https://gitter.im/puma/puma?utm\_source=badge&utm\_medium=badge&utm\_campaign=pr-badge)
[![Actions Build Status](https://github.com/puma/puma/workflows/Puma/badge.svg)](https://github.com/puma/puma/actions)
[![Travis Build Status](https://travis-ci.org/puma/puma.svg?branch=master)](https://travis-ci.org/puma/puma)

[![Code Climate](https://codeclimate.com/github/puma/puma.svg)](https://codeclimate.com/github/puma/puma)
[![SemVer](https://api.dependabot.com/badges/compatibility_score?dependency-name=puma&package-manager=bundler&version-scheme=semver)](https://dependabot.com/compatibility-score.html?dependency-name=puma&package-manager=bundler&version-scheme=semver)

Puma is a **simple, fast, multi-threaded, and highly concurrent HTTP 1.1 server for Ruby/Rack applications**.

## Built For Speed &amp; Concurrency

Puma processes requests using a C-optimized Ragel extension (inherited from Mongrel) that provides fast, accurate HTTP 1.1 protocol parsing in a portable way. Puma then serves the request using a thread pool. Each request is served in a separate thread, so truly concurrent Ruby implementations (JRuby, Rubinius) will use all available CPU cores.

Puma was designed to be the go-to server for [Rubinius](https://rubinius.com), but also works well with JRuby and MRI.

On MRI, there is a Global VM Lock (GVL) that ensures only one thread can run Ruby code at a time. But if you're doing a lot of blocking IO (such as HTTP calls to external APIs like Twitter), Puma still improves MRI's throughput by allowing IO waiting to be done in parallel.

## Quick Start

```
$ gem install puma
$ puma
```

Without arguments, puma will look for a rackup (.ru) file in the current working directory called `config.ru`.

## Frameworks

### Rails

Puma is the default server for Rails, included in the generated Gemfile.

Start your server with the `rails` command:

```
$ rails server
```

Many configuration options and Puma features are not available when using `rails server`. It is recommended that you use Puma's executable instead:

```
$ bundle exec puma
```

### Sinatra

You can run your Sinatra application with Puma from the command line like this:

```
$ ruby app.rb -s Puma
```

Or you can configure your Sinatra application to always use Puma:

```ruby
require 'sinatra'
configure { set :server, :puma }
```

## Configuration

Puma provides numerous options. Consult `puma -h` (or `puma --help`) for a full list of CLI options, or see [dsl.rb](https://github.com/puma/puma/blob/master/lib/puma/dsl.rb).

You can also find several configuration examples as part of the
[test](test/config) suite.

### Thread Pool

Puma uses a thread pool. You can set the minimum and maximum number of threads that are available in the pool with the `-t` (or `--threads`) flag:

```
$ puma -t 8:32
```

Puma will automatically scale the number of threads, from the minimum until it caps out at the maximum, based on how much traffic is present. The current default is `0:16`. Feel free to experiment, but be careful not to set the number of maximum threads to a large number, as you may exhaust resources on the system (or cause contention for the Global VM Lock, when using MRI).

Be aware that additionally Puma creates threads on its own for internal purposes (e.g. handling slow clients). So, even if you specify -t 1:1, expect around 7 threads created in your application.

### Clustered mode

Puma also offers "clustered mode". Clustered mode `fork`s workers from a master process. Each child process still has its own thread pool. You can tune the number of workers with the `-w` (or `--workers`) flag:

```
$ puma -t 8:32 -w 3
```

Note that threads are still used in clustered mode, and the `-t` thread flag setting is per worker, so `-w 2 -t 16:16` will spawn 32 threads in total, with 16 in each worker process.

In clustered mode, Puma can "preload" your application. This loads all the application code *prior* to forking. Preloading reduces total memory usage of your application via an operating system feature called [copy-on-write](https://en.wikipedia.org/wiki/Copy-on-write) (Ruby 2.0+ only). Use the `--preload` flag from the command line:

```
$ puma -w 3 --preload
```

If you're using a configuration file, use the `preload_app!` method:

```ruby
# config/puma.rb
workers 3
preload_app!
```

Additionally, you can specify a block in your configuration file that will be run on boot of each worker:

```ruby
# config/puma.rb
on_worker_boot do
  # configuration here
end
```

This code can be used to setup the process before booting the application, allowing
you to do some Puma-specific things that you don't want to embed in your application.
For instance, you could fire a log notification that a worker booted or send something to statsd. This can be called multiple times.

`before_fork` specifies a block to be run before workers are forked:

```ruby
# config/puma.rb
before_fork do
  # configuration here
end
```

Preloading can’t be used with phased restart, since phased restart kills and restarts workers one-by-one, and preload_app copies the code of master into the workers.

### Error handling

If puma encounters an error outside of the context of your application, it will respond with a 500 and a simple
textual error message (see `lowlevel_error` in [this file](https://github.com/puma/puma/blob/master/lib/puma/server.rb)).
You can specify custom behavior for this scenario. For example, you can report the error to your third-party
error-tracking service (in this example, [rollbar](http://rollbar.com)):

```ruby
lowlevel_error_handler do |e|
  Rollbar.critical(e)
  [500, {}, ["An error has occurred, and engineers have been informed. Please reload the page. If you continue to have problems, contact support@example.com\n"]]
end
```

### Binding TCP / Sockets

Bind Puma to a socket with the `-b` (or `--bind`) flag:

```
$ puma -b tcp://127.0.0.1:9292
```

To use a UNIX Socket instead of TCP:

```
$ puma -b unix:///var/run/puma.sock
```

If you need to change the permissions of the UNIX socket, just add a umask parameter:

```
$ puma -b 'unix:///var/run/puma.sock?umask=0111'
```

Need a bit of security? Use SSL sockets:

```
$ puma -b 'ssl://127.0.0.1:9292?key=path_to_key&cert=path_to_cert'
```

#### Controlling SSL Cipher Suites

To use or avoid specific SSL cipher suites, use `ssl_cipher_filter` or `ssl_cipher_list` options.

##### Ruby:

```
$ puma -b 'ssl://127.0.0.1:9292?key=path_to_key&cert=path_to_cert&ssl_cipher_filter=!aNULL:AES+SHA'
```

##### JRuby:

```
$ puma -b 'ssl://127.0.0.1:9292?keystore=path_to_keystore&keystore-pass=keystore_password&ssl_cipher_list=TLS_RSA_WITH_AES_128_CBC_SHA,TLS_RSA_WITH_AES_256_CBC_SHA'
```

See https://www.openssl.org/docs/man1.0.2/apps/ciphers.html for cipher filter format and full list of cipher suites.

Disable TLS v1 with the `no_tlsv1` option:

```
$ puma -b 'ssl://127.0.0.1:9292?key=path_to_key&cert=path_to_cert&no_tlsv1=true'
```

### Control/Status Server

Puma has a built-in status and control app that can be used to query and control Puma.

```
$ puma --control-url tcp://127.0.0.1:9293 --control-token foo
```

Puma will start the control server on localhost port 9293. All requests to the control server will need to include control token (in this case, `token=foo`) as a query parameter. This allows for simple authentication. Check out [status.rb](https://github.com/puma/puma/blob/master/lib/puma/app/status.rb) to see what the status app has available.

You can also interact with the control server via `pumactl`. This command will restart Puma:

```
$ pumactl --control-url 'tcp://127.0.0.1:9293' --control-token foo restart
```

To see a list of `pumactl` options, use `pumactl --help`.

### Configuration File

You can also provide a configuration file with the `-C` (or `--config`) flag:

```
$ puma -C /path/to/config
```

If no configuration file is specified, Puma will look for a configuration file at `config/puma.rb`. If an environment is specified, either via the `-e` and `--environment` flags, or through the `RACK_ENV` environment variable, Puma looks for configuration at `config/puma/<environment_name>.rb`.

If you want to prevent Puma from looking for a configuration file in those locations, provide a dash as the argument to the `-C` (or `--config`) flag:

```
$ puma -C "-"
```

The other side-effects of setting the environment are whether to show stack traces (in `development` or `test`), and setting RACK_ENV may potentially affect middleware looking for this value to change their behavior. The default puma RACK_ENV value is `development`. You can see all config default values [here](https://github.com/puma/puma/blob/12d1706ddc71b89ed2ee26275e31c788e94ff541/lib/puma/configuration.rb#L170).

Check out [dsl.rb](https://github.com/puma/puma/blob/master/lib/puma/dsl.rb) to see all available options.

## Restart

Puma includes the ability to restart itself. When available (MRI, Rubinius, JRuby), Puma performs a "hot restart". This is the same functionality available in *Unicorn* and *NGINX* which keep the server sockets open between restarts. This makes sure that no pending requests are dropped while the restart is taking place.

For more, see the [restart documentation](https://github.com/puma/puma/blob/master/docs/restart.md).

## Signals

Puma responds to several signals. A detailed guide to using UNIX signals with Puma can be found in the [signals documentation](https://github.com/puma/puma/blob/master/docs/signals.md).

## Platform Constraints

Some platforms do not support all Puma features.

  * **JRuby**, **Windows**: server sockets are not seamless on restart, they must be closed and reopened. These platforms have no way to pass descriptors into a new process that is exposed to Ruby. Also, cluster mode is not supported due to a lack of fork(2).
  * **Windows**: Cluster mode is not supported due to a lack of fork(2).

## Known Bugs

For MRI versions 2.2.7, 2.2.8, 2.2.9, 2.2.10 2.3.4 and 2.4.1, you may see ```stream closed in another thread (IOError)```. It may be caused by a [Ruby bug](https://bugs.ruby-lang.org/issues/13632). It can be fixed with the gem https://rubygems.org/gems/stopgap_13632:

```ruby
if %w(2.2.7 2.2.8 2.2.9 2.2.10 2.3.4 2.4.1).include? RUBY_VERSION
  begin
    require 'stopgap_13632'
  rescue LoadError
  end
end
```

## Deployment

Puma has support for Capistrano with an [external gem](https://github.com/seuros/capistrano-puma).

It is common to use process monitors with Puma. Modern process monitors like systemd or upstart
provide continuous monitoring and restarts for increased
reliability in production environments:

* [tools/jungle](https://github.com/puma/puma/tree/master/tools/jungle) for sysvinit (init.d) and upstart
* [docs/systemd](https://github.com/puma/puma/blob/master/docs/systemd.md)

## Community Plugins

* [puma-heroku](https://github.com/evanphx/puma-heroku) — default Puma configuration for running on Heroku
* [puma-metrics](https://github.com/harmjanblok/puma-metrics) — export Puma metrics to Prometheus
* [puma-plugin-statsd](https://github.com/yob/puma-plugin-statsd) — send Puma metrics to statsd
* [puma-plugin-systemd](https://github.com/sj26/puma-plugin-systemd) — deeper integration with systemd for notify, status and watchdog

## Contributing

Find details for contributing in the [contribution guide].

[contribution guide]: https://github.com/puma/puma/blob/master/CONTRIBUTING.md

## License

Puma is copyright Evan Phoenix and contributors, licensed under the BSD 3-Clause license. See the included LICENSE file for details.
