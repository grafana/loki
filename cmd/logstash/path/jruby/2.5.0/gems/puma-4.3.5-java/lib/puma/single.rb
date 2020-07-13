# frozen_string_literal: true

require 'puma/runner'
require 'puma/detect'
require 'puma/plugin'

module Puma
  # This class is instantiated by the `Puma::Launcher` and used
  # to boot and serve a Ruby application when no puma "workers" are needed
  # i.e. only using "threaded" mode. For example `$ puma -t 1:5`
  #
  # At the core of this class is running an instance of `Puma::Server` which
  # gets created via the `start_server` method from the `Puma::Runner` class
  # that this inherits from.
  class Single < Runner
    def stats
      b = @server.backlog || 0
      r = @server.running || 0
      t = @server.pool_capacity || 0
      m = @server.max_threads || 0
      %Q!{ "started_at": "#{@started_at.utc.iso8601}", "backlog": #{b}, "running": #{r}, "pool_capacity": #{t}, "max_threads": #{m} }!
    end

    def restart
      @server.begin_restart
    end

    def stop
      @server.stop(false) if @server
    end

    def halt
      @server.halt
    end

    def stop_blocked
      log "- Gracefully stopping, waiting for requests to finish"
      @control.stop(true) if @control
      @server.stop(true) if @server
    end

    def jruby_daemon?
      daemon? and Puma.jruby?
    end

    def jruby_daemon_start
      require 'puma/jruby_restart'
      JRubyRestart.daemon_start(@restart_dir, @launcher.restart_args)
    end

    def run
      already_daemon = false

      if jruby_daemon?
        require 'puma/jruby_restart'

        if JRubyRestart.daemon?
          # load and bind before redirecting IO so errors show up on stdout/stderr
          load_and_bind
          redirect_io
        end

        already_daemon = JRubyRestart.daemon_init
      end

      output_header "single"

      if jruby_daemon?
        if already_daemon
          JRubyRestart.perm_daemonize
        else
          pid = nil

          Signal.trap "SIGUSR2" do
            log "* Started new process #{pid} as daemon..."

            # Must use exit! so we don't unwind and run the ensures
            # that will be run by the new child (such as deleting the
            # pidfile)
            exit!(true)
          end

          Signal.trap "SIGCHLD" do
            log "! Error starting new process as daemon, exiting"
            exit 1
          end

          jruby_daemon_start
          sleep
        end
      else
        if daemon?
          log "* Daemonizing..."
          Process.daemon(true)
          redirect_io
        end

        load_and_bind
      end

      Plugins.fire_background

      @launcher.write_state

      start_control

      @server = server = start_server

      unless daemon?
        log "Use Ctrl-C to stop"
        redirect_io
      end

      @launcher.events.fire_on_booted!

      begin
        server.run.join
      rescue Interrupt
        # Swallow it
      end
    end
  end
end
