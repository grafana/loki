# frozen_string_literal: true

module Rack
  # Rack::Builder implements a small DSL to iteratively construct Rack
  # applications.
  #
  # Example:
  #
  #  require 'rack/lobster'
  #  app = Rack::Builder.new do
  #    use Rack::CommonLogger
  #    use Rack::ShowExceptions
  #    map "/lobster" do
  #      use Rack::Lint
  #      run Rack::Lobster.new
  #    end
  #  end
  #
  #  run app
  #
  # Or
  #
  #  app = Rack::Builder.app do
  #    use Rack::CommonLogger
  #    run lambda { |env| [200, {'Content-Type' => 'text/plain'}, ['OK']] }
  #  end
  #
  #  run app
  #
  # +use+ adds middleware to the stack, +run+ dispatches to an application.
  # You can use +map+ to construct a Rack::URLMap in a convenient way.

  class Builder

    # https://stackoverflow.com/questions/2223882/whats-the-difference-between-utf-8-and-utf-8-without-bom
    UTF_8_BOM = '\xef\xbb\xbf'

    # Parse the given config file to get a Rack application.
    #
    # If the config file ends in +.ru+, it is treated as a
    # rackup file and the contents will be treated as if
    # specified inside a Rack::Builder block, using the given
    # options.
    #
    # If the config file does not end in +.ru+, it is
    # required and Rack will use the basename of the file
    # to guess which constant will be the Rack application to run.
    # The options given will be ignored in this case.
    #
    # Examples:
    #
    #   Rack::Builder.parse_file('config.ru')
    #   # Rack application built using Rack::Builder.new
    #
    #   Rack::Builder.parse_file('app.rb')
    #   # requires app.rb, which can be anywhere in Ruby's
    #   # load path. After requiring, assumes App constant
    #   # contains Rack application
    #
    #   Rack::Builder.parse_file('./my_app.rb')
    #   # requires ./my_app.rb, which should be in the
    #   # process's current directory.  After requiring,
    #   # assumes MyApp constant contains Rack application
    def self.parse_file(config, opts = Server::Options.new)
      if config.end_with?('.ru')
        return self.load_file(config, opts)
      else
        require config
        app = Object.const_get(::File.basename(config, '.rb').split('_').map(&:capitalize).join(''))
        return app, {}
      end
    end

    # Load the given file as a rackup file, treating the
    # contents as if specified inside a Rack::Builder block.
    #
    # Treats the first comment at the beginning of a line
    # that starts with a backslash as options similar to
    # options passed on a rackup command line.
    #
    # Ignores content in the file after +__END__+, so that
    # use of +__END__+ will not result in a syntax error.
    #
    # Example config.ru file:
    #
    #   $ cat config.ru
    #
    #   #\ -p 9393
    #
    #   use Rack::ContentLength
    #   require './app.rb'
    #   run App
    def self.load_file(path, opts = Server::Options.new)
      options = {}

      cfgfile = ::File.read(path)
      cfgfile.slice!(/\A#{UTF_8_BOM}/) if cfgfile.encoding == Encoding::UTF_8

      if cfgfile[/^#\\(.*)/] && opts
        warn "Parsing options from the first comment line is deprecated!"
        options = opts.parse! $1.split(/\s+/)
      end

      cfgfile.sub!(/^__END__\n.*\Z/m, '')
      app = new_from_string cfgfile, path

      return app, options
    end

    # Evaluate the given +builder_script+ string in the context of
    # a Rack::Builder block, returning a Rack application.
    def self.new_from_string(builder_script, file = "(rackup)")
      # We want to build a variant of TOPLEVEL_BINDING with self as a Rack::Builder instance.
      # We cannot use instance_eval(String) as that would resolve constants differently.
      binding, builder = TOPLEVEL_BINDING.eval('Rack::Builder.new.instance_eval { [binding, self] }')
      eval builder_script, binding, file
      builder.to_app
    end

    # Initialize a new Rack::Builder instance.  +default_app+ specifies the
    # default application if +run+ is not called later.  If a block
    # is given, it is evaluted in the context of the instance.
    def initialize(default_app = nil, &block)
      @use, @map, @run, @warmup, @freeze_app = [], nil, default_app, nil, false
      instance_eval(&block) if block_given?
    end

    # Create a new Rack::Builder instance and return the Rack application
    # generated from it.
    def self.app(default_app = nil, &block)
      self.new(default_app, &block).to_app
    end

    # Specifies middleware to use in a stack.
    #
    #   class Middleware
    #     def initialize(app)
    #       @app = app
    #     end
    #
    #     def call(env)
    #       env["rack.some_header"] = "setting an example"
    #       @app.call(env)
    #     end
    #   end
    #
    #   use Middleware
    #   run lambda { |env| [200, { "Content-Type" => "text/plain" }, ["OK"]] }
    #
    # All requests through to this application will first be processed by the middleware class.
    # The +call+ method in this example sets an additional environment key which then can be
    # referenced in the application if required.
    def use(middleware, *args, &block)
      if @map
        mapping, @map = @map, nil
        @use << proc { |app| generate_map(app, mapping) }
      end
      @use << proc { |app| middleware.new(app, *args, &block) }
    end
    ruby2_keywords(:use) if respond_to?(:ruby2_keywords, true)

    # Takes an argument that is an object that responds to #call and returns a Rack response.
    # The simplest form of this is a lambda object:
    #
    #   run lambda { |env| [200, { "Content-Type" => "text/plain" }, ["OK"]] }
    #
    # However this could also be a class:
    #
    #   class Heartbeat
    #     def self.call(env)
    #      [200, { "Content-Type" => "text/plain" }, ["OK"]]
    #     end
    #   end
    #
    #   run Heartbeat
    def run(app)
      @run = app
    end

    # Takes a lambda or block that is used to warm-up the application. This block is called
    # before the Rack application is returned by to_app.
    #
    #   warmup do |app|
    #     client = Rack::MockRequest.new(app)
    #     client.get('/')
    #   end
    #
    #   use SomeMiddleware
    #   run MyApp
    def warmup(prc = nil, &block)
      @warmup = prc || block
    end

    # Creates a route within the application.  Routes under the mapped path will be sent to
    # the Rack application specified by run inside the block.  Other requests will be sent to the
    # default application specified by run outside the block.
    #
    #   Rack::Builder.app do
    #     map '/heartbeat' do
    #       run Heartbeat
    #     end
    #     run App
    #   end
    #
    # The +use+ method can also be used inside the block to specify middleware to run under a specific path:
    #
    #   Rack::Builder.app do
    #     map '/heartbeat' do
    #       use Middleware
    #       run Heartbeat
    #     end
    #     run App
    #   end
    #
    # This example includes a piece of middleware which will run before +/heartbeat+ requests hit +Heartbeat+.
    #
    # Note that providing a +path+ of +/+ will ignore any default application given in a +run+ statement
    # outside the block.
    def map(path, &block)
      @map ||= {}
      @map[path] = block
    end

    # Freeze the app (set using run) and all middleware instances when building the application
    # in to_app.
    def freeze_app
      @freeze_app = true
    end

    # Return the Rack application generated by this instance.
    def to_app
      app = @map ? generate_map(@run, @map) : @run
      fail "missing run or map statement" unless app
      app.freeze if @freeze_app
      app = @use.reverse.inject(app) { |a, e| e[a].tap { |x| x.freeze if @freeze_app } }
      @warmup.call(app) if @warmup
      app
    end

    # Call the Rack application generated by this builder instance. Note that
    # this rebuilds the Rack application and runs the warmup code (if any)
    # every time it is called, so it should not be used if performance is important.
    def call(env)
      to_app.call(env)
    end

    private

    # Generate a URLMap instance by generating new Rack applications for each
    # map block in this instance.
    def generate_map(default_app, mapping)
      mapped = default_app ? { '/' => default_app } : {}
      mapping.each { |r, b| mapped[r] = self.class.new(default_app, &b).to_app }
      URLMap.new(mapped)
    end
  end
end
