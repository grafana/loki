# frozen_string_literal: true

require 'puma/const'

module Puma
  # The methods that are available for use inside the configuration file.
  # These same methods are used in Puma cli and the rack handler
  # internally.
  #
  # Used manually (via CLI class):
  #
  #   config = Configuration.new({}) do |user_config|
  #     user_config.port 3001
  #   end
  #   config.load
  #
  #   puts config.options[:binds]
  #   "tcp://127.0.0.1:3001"
  #
  # Used to load file:
  #
  #   $ cat puma_config.rb
  #     port 3002
  #
  #   config = Configuration.new(config_file: "puma_config.rb")
  #   config.load
  #
  #   puts config.options[:binds]
  #   # => "tcp://127.0.0.1:3002"
  #
  # You can also find many examples being used by the test suite in
  # +test/config+.
  class DSL
    include ConfigDefault

    def initialize(options, config)
      @config  = config
      @options = options

      @plugins = []
    end

    def _load_from(path)
      if path
        @path = path
        instance_eval(File.read(path), path, 1)
      end
    ensure
      _offer_plugins
    end

    def _offer_plugins
      @plugins.each do |o|
        if o.respond_to? :config
          @options.shift
          o.config self
        end
      end

      @plugins.clear
    end

    def set_default_host(host)
      @options[:default_host] = host
    end

    def default_host
      @options[:default_host] || Configuration::DefaultTCPHost
    end

    def inject(&blk)
      instance_eval(&blk)
    end

    def get(key,default=nil)
      @options[key.to_sym] || default
    end

    # Load the named plugin for use by this configuration
    #
    def plugin(name)
      @plugins << @config.load_plugin(name)
    end

    # Use an object or block as the rack application. This allows the
    # configuration file to be the application itself.
    #
    # @example
    #   app do |env|
    #     body = 'Hello, World!'
    #
    #     [
    #       200,
    #       {
    #         'Content-Type' => 'text/plain',
    #         'Content-Length' => body.length.to_s
    #       },
    #       [body]
    #     ]
    #   end
    def app(obj=nil, &block)
      obj ||= block

      raise "Provide either a #call'able or a block" unless obj

      @options[:app] = obj
    end

    # Start the Puma control rack application on +url+. This application can
    # be communicated with to control the main server. Additionally, you can
    # provide an authentication token, so all requests to the control server
    # will need to include that token as a query parameter. This allows for
    # simple authentication.
    #
    # Check out {Puma::App::Status} to see what the app has available.
    #
    # @example
    #   activate_control_app 'unix:///var/run/pumactl.sock'
    # @example
    #   activate_control_app 'unix:///var/run/pumactl.sock', { auth_token: '12345' }
    # @example
    #   activate_control_app 'unix:///var/run/pumactl.sock', { no_token: true }
    def activate_control_app(url="auto", opts={})
      if url == "auto"
        path = Configuration.temp_path
        @options[:control_url] = "unix://#{path}"
        @options[:control_url_temp] = path
      else
        @options[:control_url] = url
      end

      if opts[:no_token]
        # We need to use 'none' rather than :none because this value will be
        # passed on to an instance of OptionParser, which doesn't support
        # symbols as option values.
        #
        # See: https://github.com/puma/puma/issues/1193#issuecomment-305995488
        auth_token = 'none'
      else
        auth_token = opts[:auth_token]
        auth_token ||= Configuration.random_token
      end

      @options[:control_auth_token] = auth_token
      @options[:control_url_umask] = opts[:umask] if opts[:umask]
    end

    # Load additional configuration from a file
    # Files get loaded later via Configuration#load
    def load(file)
      @options[:config_files] ||= []
      @options[:config_files] << file
    end

    # Bind the server to +url+. "tcp://", "unix://" and "ssl://" are the only
    # accepted protocols. Multiple urls can be bound to, calling `bind` does
    # not overwrite previous bindings.
    #
    # The default is "tcp://0.0.0.0:9292".
    #
    # You can use query parameters within the url to specify options:
    #
    #  - Set the socket backlog depth with +backlog+, default is 1024.
    #  - Set up an SSL certificate with +key+ & +cert+.
    #  - Set whether to optimize for low latency instead of throughput with
    #    +low_latency+, default is to optimize for low latency. This is done
    #    via +Socket::TCP_NODELAY+.
    #  - Set socket permissions with +umask+.
    #
    # @example Backlog depth
    #   bind 'unix:///var/run/puma.sock?backlog=512'
    # @example SSL cert
    #   bind 'ssl://127.0.0.1:9292?key=key.key&cert=cert.pem'
    # @example Disable optimization for low latency
    #   bind 'tcp://0.0.0.0:9292?low_latency=false'
    # @example Socket permissions
    #   bind 'unix:///var/run/puma.sock?umask=0111'
    def bind(url)
      @options[:binds] ||= []
      @options[:binds] << url
    end

    def clear_binds!
      @options[:binds] = []
    end

    # Define the TCP port to bind to. Use +bind+ for more advanced options.
    #
    # @example
    #   port 9292
    def port(port, host=nil)
      host ||= default_host
      bind "tcp://#{host}:#{port}"
    end

    # Define how long persistent connections can be idle before Puma closes
    # them.
    def persistent_timeout(seconds)
      @options[:persistent_timeout] = Integer(seconds)
    end

    # Define how long the tcp socket stays open, if no data has been received.
    def first_data_timeout(seconds)
      @options[:first_data_timeout] = Integer(seconds)
    end

    # Work around leaky apps that leave garbage in Thread locals
    # across requests.
    def clean_thread_locals(which=true)
      @options[:clean_thread_locals] = which
    end

    # Daemonize the server into the background. It's highly recommended to
    # use this in combination with +pidfile+ and +stdout_redirect+.
    #
    # The default is "false".
    #
    # @example
    #   daemonize
    #
    # @example
    #   daemonize false
    def daemonize(which=true)
      @options[:daemon] = which
    end

    # When shutting down, drain the accept socket of pending
    # connections and process them. This loops over the accept
    # socket until there are no more read events and then stops
    # looking and waits for the requests to finish.
    def drain_on_shutdown(which=true)
      @options[:drain_on_shutdown] = which
    end

    # Set the environment in which the rack's app will run. The value must be
    # a string.
    #
    # The default is "development".
    #
    # @example
    #   environment 'production'
    def environment(environment)
      @options[:environment] = environment
    end

    # How long to wait for threads to stop when shutting them
    # down. Defaults to :forever. Specifying :immediately will cause
    # Puma to kill the threads immediately.  Otherwise the value
    # is the number of seconds to wait.
    #
    # Puma always waits a few seconds after killing a thread for it to try
    # to finish up it's work, even in :immediately mode.
    def force_shutdown_after(val=:forever)
      i = case val
          when :forever
            -1
          when :immediately
            0
          else
            Integer(val)
          end

      @options[:force_shutdown_after] = i
    end

    # Code to run before doing a restart. This code should
    # close log files, database connections, etc.
    #
    # This can be called multiple times to add code each time.
    #
    # @example
    #   on_restart do
    #     puts 'On restart...'
    #   end
    def on_restart(&block)
      @options[:on_restart] ||= []
      @options[:on_restart] << block
    end

    # Command to use to restart Puma. This should be just how to
    # load Puma itself (ie. 'ruby -Ilib bin/puma'), not the arguments
    # to Puma, as those are the same as the original process.
    #
    # @example
    #   restart_command '/u/app/lolcat/bin/restart_puma'
    def restart_command(cmd)
      @options[:restart_cmd] = cmd.to_s
    end

    # Store the pid of the server in the file at "path".
    #
    # @example
    #   pidfile '/u/apps/lolcat/tmp/pids/puma.pid'
    def pidfile(path)
      @options[:pidfile] = path.to_s
    end

    # Disable request logging, if this isn't used it'll be enabled by default.
    #
    # @example
    #   quiet
    def quiet(which=true)
      @options[:log_requests] = !which
    end

    # Enable request logging
    #
    def log_requests(which=true)
      @options[:log_requests] = which
    end

    # Show debugging info
    #
    def debug
      @options[:debug] = true
    end

    # Load +path+ as a rackup file.
    #
    # The default is "config.ru".
    #
    # @example
    #   rackup '/u/apps/lolcat/config.ru'
    def rackup(path)
      @options[:rackup] = path.to_s
    end

    # Run Puma in TCP mode
    #
    def tcp_mode!
      @options[:mode] = :tcp
    end

    def early_hints(answer=true)
      @options[:early_hints] = answer
    end

    # Redirect STDOUT and STDERR to files specified. The +append+ parameter
    # specifies whether the output is appended, the default is +false+.
    #
    # @example
    #   stdout_redirect '/app/lolcat/log/stdout', '/app/lolcat/log/stderr'
    # @example
    #   stdout_redirect '/app/lolcat/log/stdout', '/app/lolcat/log/stderr', true
    def stdout_redirect(stdout=nil, stderr=nil, append=false)
      @options[:redirect_stdout] = stdout
      @options[:redirect_stderr] = stderr
      @options[:redirect_append] = append
    end

    def log_formatter(&block)
      @options[:log_formatter] = block
    end

    # Configure +min+ to be the minimum number of threads to use to answer
    # requests and +max+ the maximum.
    #
    # The default is "0, 16".
    #
    # @example
    #   threads 0, 16
    # @example
    #   threads 5, 5
    def threads(min, max)
      min = Integer(min)
      max = Integer(max)
      if min > max
        raise "The minimum (#{min}) number of threads must be less than or equal to the max (#{max})"
      end

      if max < 1
        raise "The maximum number of threads (#{max}) must be greater than 0"
      end

      @options[:min_threads] = min
      @options[:max_threads] = max
    end

    # Instead of "bind 'ssl://127.0.0.1:9292?key=key_path&cert=cert_path'" you
    # can also use the "ssl_bind" option.
    #
    # @example
    #   ssl_bind '127.0.0.1', '9292', {
    #     cert: path_to_cert,
    #     key: path_to_key,
    #     ssl_cipher_filter: cipher_filter, # optional
    #     verify_mode: verify_mode,         # default 'none'
    #   }
    # @example For JRuby additional keys are required: keystore & keystore_pass.
    #   ssl_bind '127.0.0.1', '9292', {
    #     cert: path_to_cert,
    #     key: path_to_key,
    #     ssl_cipher_filter: cipher_filter, # optional
    #     verify_mode: verify_mode,         # default 'none'
    #     keystore: path_to_keystore,
    #     keystore_pass: password
    #   }
    def ssl_bind(host, port, opts)
      verify = opts.fetch(:verify_mode, 'none').to_s
      no_tlsv1 = opts.fetch(:no_tlsv1, 'false')
      no_tlsv1_1 = opts.fetch(:no_tlsv1_1, 'false')
      ca_additions = "&ca=#{opts[:ca]}" if ['peer', 'force_peer'].include?(verify)

      if defined?(JRUBY_VERSION)
        keystore_additions = "keystore=#{opts[:keystore]}&keystore-pass=#{opts[:keystore_pass]}"
        bind "ssl://#{host}:#{port}?cert=#{opts[:cert]}&key=#{opts[:key]}&#{keystore_additions}&verify_mode=#{verify}&no_tlsv1=#{no_tlsv1}&no_tlsv1_1=#{no_tlsv1_1}#{ca_additions}"
      else
        ssl_cipher_filter = "&ssl_cipher_filter=#{opts[:ssl_cipher_filter]}" if opts[:ssl_cipher_filter]
        bind "ssl://#{host}:#{port}?cert=#{opts[:cert]}&key=#{opts[:key]}#{ssl_cipher_filter}&verify_mode=#{verify}&no_tlsv1=#{no_tlsv1}&no_tlsv1_1=#{no_tlsv1_1}#{ca_additions}"
      end
    end

    # Use +path+ as the file to store the server info state. This is
    # used by +pumactl+ to query and control the server.
    #
    # @example
    #   state_path '/u/apps/lolcat/tmp/pids/puma.state'
    def state_path(path)
      @options[:state] = path.to_s
    end

    # How many worker processes to run.  Typically this is set to
    # to the number of available cores.
    #
    # The default is 0.
    #
    # @note Cluster mode only.
    def workers(count)
      @options[:workers] = count.to_i
    end

    # Code to run immediately before master process
    # forks workers (once on boot). These hooks can block if necessary
    # to wait for background operations unknown to Puma to finish before
    # the process terminates.
    # This can be used to close any connections to remote servers (database,
    # Redis, ...) that were opened when preloading the code.
    #
    # This can be called multiple times to add several hooks.
    #
    # @note Cluster mode only.
    # @example
    #   before_fork do
    #     puts "Starting workers..."
    #   end
    def before_fork(&block)
      @options[:before_fork] ||= []
      @options[:before_fork] << block
    end

    # Code to run in a worker when it boots to setup
    # the process before booting the app.
    #
    # This can be called multiple times to add several hooks.
    #
    # @note Cluster mode only.
    # @example
    #   on_worker_fork do
    #     puts 'Before worker fork...'
    #   end
    def on_worker_boot(&block)
      @options[:before_worker_boot] ||= []
      @options[:before_worker_boot] << block
    end

    # Code to run immediately before a worker shuts
    # down (after it has finished processing HTTP requests). These hooks
    # can block if necessary to wait for background operations unknown
    # to Puma to finish before the process terminates.
    #
    # This can be called multiple times to add several hooks.
    #
    # @note Cluster mode only.
    # @example
    #   on_worker_shutdown do
    #     puts 'On worker shutdown...'
    #   end
    def on_worker_shutdown(&block)
      @options[:before_worker_shutdown] ||= []
      @options[:before_worker_shutdown] << block
    end

    # Code to run in the master right before a worker is started. The worker's
    # index is passed as an argument.
    #
    # This can be called multiple times to add several hooks.
    #
    # @note Cluster mode only.
    # @example
    #   on_worker_fork do
    #     puts 'Before worker fork...'
    #   end
    def on_worker_fork(&block)
      @options[:before_worker_fork] ||= []
      @options[:before_worker_fork] << block
    end

    # Code to run in the master after a worker has been started. The worker's
    # index is passed as an argument.
    #
    # This is called everytime a worker is to be started.
    #
    # @note Cluster mode only.
    # @example
    #   after_worker_fork do
    #     puts 'After worker fork...'
    #   end
    def after_worker_fork(&block)
      @options[:after_worker_fork] ||= []
      @options[:after_worker_fork] = block
    end

    alias_method :after_worker_boot, :after_worker_fork

    # Code to run out-of-band when the worker is idle.
    # These hooks run immediately after a request has finished
    # processing and there are no busy threads on the worker.
    # The worker doesn't accept new requests until this code finishes.
    #
    # This hook is useful for running out-of-band garbage collection
    # or scheduling asynchronous tasks to execute after a response.
    #
    # This can be called multiple times to add several hooks.
    def out_of_band(&block)
      @options[:out_of_band] ||= []
      @options[:out_of_band] << block
    end

    # The directory to operate out of.
    #
    # The default is the current directory.
    #
    # @example
    #   directory '/u/apps/lolcat'
    def directory(dir)
      @options[:directory] = dir.to_s
    end

    # DEPRECATED: The directory to operate out of.
    def worker_directory(dir)
      $stderr.puts "worker_directory is deprecated. Please use `directory`"
      directory dir
    end

    # Run the app as a raw TCP app instead of an HTTP rack app.
    def tcp_mode
      @options[:mode] = :tcp
    end

    # Preload the application before starting the workers; this conflicts with
    # phased restart feature. This is off by default.
    #
    # @note Cluster mode only.
    # @example
    #   preload_app!
    def preload_app!(answer=true)
      @options[:preload_app] = answer
    end

    # Use +obj+ or +block+ as the low level error handler. This allows the
    # configuration file to change the default error on the server.
    #
    # @example
    #   lowlevel_error_handler do |err|
    #     [200, {}, ["error page"]]
    #   end
    def lowlevel_error_handler(obj=nil, &block)
      obj ||= block
      raise "Provide either a #call'able or a block" unless obj
      @options[:lowlevel_error_handler] = obj
    end

    # This option is used to allow your app and its gems to be
    # properly reloaded when not using preload.
    #
    # When set, if Puma detects that it's been invoked in the
    # context of Bundler, it will cleanup the environment and
    # re-run itself outside the Bundler environment, but directly
    # using the files that Bundler has setup.
    #
    # This means that Puma is now decoupled from your Bundler
    # context and when each worker loads, it will be loading a
    # new Bundler context and thus can float around as the release
    # dictates.
    #
    # See also: extra_runtime_dependencies
    #
    # @note This is incompatible with +preload_app!+.
    # @note This is only supported for RubyGems 2.2+
    def prune_bundler(answer=true)
      @options[:prune_bundler] = answer
    end

    # By default, Puma will raise SignalException when SIGTERM is received. In
    # environments where SIGTERM is something expected, you can suppress these
    # with this option.
    #
    # This can be useful for example in Kubernetes, where rolling restart is
    # guaranteed usually on infrastructure level.
    #
    # @example
    #   raise_exception_on_sigterm false
    def raise_exception_on_sigterm(answer=true)
      @options[:raise_exception_on_sigterm] = answer
    end

    # When using prune_bundler, if extra runtime dependencies need to be loaded to
    # initialize your app, then this setting can be used. This includes any Puma plugins.
    #
    # Before bundler is pruned, the gem names supplied will be looked up in the bundler
    # context and then loaded again after bundler is pruned.
    # Only applies if prune_bundler is used.
    #
    # @example
    #   extra_runtime_dependencies ['gem_name_1', 'gem_name_2']
    # @example
    #   extra_runtime_dependencies ['puma_worker_killer', 'puma-heroku']
    def extra_runtime_dependencies(answer = [])
      @options[:extra_runtime_dependencies] = Array(answer)
    end

    # Additional text to display in process listing.
    #
    # If you do not specify a tag, Puma will infer it. If you do not want Puma
    # to add a tag, use an empty string.
    #
    # @example
    #   tag 'app name'
    # @example
    #   tag ''
    def tag(string)
      @options[:tag] = string.to_s
    end

    # Verifies that all workers have checked in to the master process within
    # the given timeout. If not the worker process will be restarted. This is
    # not a request timeout, it is to protect against a hung or dead process.
    # Setting this value will not protect against slow requests.
    #
    # The minimum value is 6 seconds, the default value is 60 seconds.
    #
    # @note Cluster mode only.
    # @example
    #   worker_timeout 60
    def worker_timeout(timeout)
      timeout = Integer(timeout)
      min = Const::WORKER_CHECK_INTERVAL

      if timeout <= min
        raise "The minimum worker_timeout must be greater than the worker reporting interval (#{min})"
      end

      @options[:worker_timeout] = timeout
    end

    # Change the default worker timeout for booting.
    #
    # If unspecified, this defaults to the value of worker_timeout.
    #
    # @note Cluster mode only.
    # @example:
    #   worker_boot_timeout 60
    def worker_boot_timeout(timeout)
      @options[:worker_boot_timeout] = Integer(timeout)
    end

    # Set the timeout for worker shutdown
    #
    # @note Cluster mode only.
    def worker_shutdown_timeout(timeout)
      @options[:worker_shutdown_timeout] = Integer(timeout)
    end

    # When set to true (the default), workers accept all requests
    # and queue them before passing them to the handlers.
    # When set to false, each worker process accepts exactly as
    # many requests as it is configured to simultaneously handle.
    #
    # Queueing requests generally improves performance. In some
    # cases, such as a single threaded application, it may be
    # better to ensure requests get balanced across workers.
    #
    # Note that setting this to false disables HTTP keepalive and
    # slow clients will occupy a handler thread while the request
    # is being sent. A reverse proxy, such as nginx, can handle
    # slow clients and queue requests before they reach Puma.
    def queue_requests(answer=true)
      @options[:queue_requests] = answer
    end

    # When a shutdown is requested, the backtraces of all the
    # threads will be written to $stdout. This can help figure
    # out why shutdown is hanging.
    def shutdown_debug(val=true)
      @options[:shutdown_debug] = val
    end

    # Control how the remote address of the connection is set. This
    # is configurable because to calculate the true socket peer address
    # a kernel syscall is required which for very fast rack handlers
    # slows down the handling significantly.
    #
    # There are 4 possible values:
    #
    # * :socket (the default) - read the peername from the socket using the
    #           syscall. This is the normal behavior.
    # * :localhost - set the remote address to "127.0.0.1"
    # * header: http_header - set the remote address to the value of the
    #                          provided http header. For instance:
    #                          `set_remote_address header: "X-Real-IP"`.
    #                          Only the first word (as separated by spaces or comma)
    #                          is used, allowing headers such as X-Forwarded-For
    #                          to be used as well.
    # * Any string - this allows you to hardcode remote address to any value
    #                you wish. Because Puma never uses this field anyway, it's
    #                format is entirely in your hands.
    def set_remote_address(val=:socket)
      case val
      when :socket
        @options[:remote_address] = val
      when :localhost
        @options[:remote_address] = :value
        @options[:remote_address_value] = "127.0.0.1".freeze
      when String
        @options[:remote_address] = :value
        @options[:remote_address_value] = val
      when Hash
        if hdr = val[:header]
          @options[:remote_address] = :header
          @options[:remote_address_header] = "HTTP_" + hdr.upcase.tr("-", "_")
        else
          raise "Invalid value for set_remote_address - #{val.inspect}"
        end
      else
        raise "Invalid value for set_remote_address - #{val}"
      end
    end

  end
end
