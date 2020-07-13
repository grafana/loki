# Deployment engineering for puma

Puma is software that is expected to be run in a deployed environment eventually.
You can certainly use it as your dev server only, but most people look to use
it in their production deployments as well.

To that end, this is meant to serve as a foundation of wisdom how to do that
in a way that increases happiness and decreases downtime.

## Specifying puma

Most people want to do this by putting `gem "puma"` into their Gemfile, so we'll
go ahead and assume that. Go add it now... we'll wait.


Welcome back!

## Single vs Cluster mode

Puma was originally conceived as a thread-only webserver, but grew the ability to
also use processes in version 2.

Here are some rules of thumb:

### MRI

* Use cluster mode and set the number of workers to 1.5x the number of cpu cores
  in the machine, minimum 2.
* Set the number of threads to desired concurrent requests / number of workers.
  Puma defaults to 16 and that's a decent number.

#### Migrating from Unicorn

* If you're migrating from unicorn though, here are some settings to start with:
  * Set workers to half the number of unicorn workers you're using
  * Set threads to 2
  * Enjoy 50% memory savings
* As you grow more confident in the thread safety of your app, you can tune the
  workers down and the threads up.

#### Ubuntu / Systemd (Systemctl) Installation

See [systemd.md](systemd.md)

#### Worker utilization

**How do you know if you've got enough (or too many workers)?**

A good question. Due to MRI's GIL, only one thread can be executing Ruby code at a time.
But since so many apps are waiting on IO from DBs, etc., they can utilize threads
to make better use of the process.

The rule of thumb is you never want processes that are pegged all the time. This
means that there is more work to do than the process can get through. On the other
hand, if you have processes that sit around doing nothing, then they're just eating
up resources.

Watch your CPU utilization over time and aim for about 70% on average. This means
you've got capacity still but aren't starving threads.

**Measuring utilization**

Using a timestamp header from an upstream proxy server (eg. nginx or haproxy), it's
possible to get an indication of how long requests have been waiting for a Puma
thread to become available.

* Have your upstream proxy set a header with the time it received the request:
    * nginx: `proxy_set_header X-Request-Start "${msec}";`
    * haproxy: `http-request set-header X-Request-Start "%t";`
* In your Rack middleware, determine the amount of time elapsed since `X-Request-Start`.
* To improve accuracy, you will want to subtract time spent waiting for slow clients:
    * `env['puma.request_body_wait']` contains the number of milliseconds Puma spent
      waiting for the client to send the request body.
    * haproxy: `%Th` (TLS handshake time) and `%Ti` (idle time before request) can
      can also be added as headers.

## Daemonizing

I prefer to not daemonize my servers and use something like `runit` or `upstart` to
monitor them as child processes. This gives them fast response to crashes and
makes it easy to figure out what is going on. Additionally, unlike `unicorn`,
puma does not require daemonization to do zero-downtime restarts.

I see people using daemonization because they start puma directly via capistrano
task and thus want it to live on past the `cap deploy`. To these people I say:
You need to be using a process monitor. Nothing is making sure puma stays up in
this scenario! You're just waiting for something weird to happen, puma to die,
and to get paged at 3am. Do yourself a favor, at least the process monitoring
your OS comes with, be it `sysvinit`, `upstart`, or `systemd`. Or branch out
and use `runit` or hell, even `monit`.

## Restarting

You probably will want to deploy some new code at some point, and you'd like
puma to start running that new code. Minimizing the amount of time the server
is unavailable would be nice as well. Here's how to do it:

1. Don't use `preload!`. This dirties the master process and means it will have
to shutdown all the workers and re-exec itself to get your new code. It is not compatible with phased-restart and `prune_bundler` as well.

1. Use `prune_bundler`. This makes it so that the cluster master will detach itself
from a Bundler context on start. This allows the cluster workers to load your app
and start a brand new Bundler context within the worker only. This means your
master remains pristine and can live on between new releases of your code.

1. Use phased-restart (`SIGUSR1` or `pumactl phased-restart`). This tells the master
to kill off one worker at a time and restart them in your new code. This minimizes
downtime and staggers the restart nicely. **WARNING** This means that both your
old code and your new code will be running concurrently. Most deployment solutions
already cause that, but it's worth warning you about it again. Be careful with your
migrations, etc!
