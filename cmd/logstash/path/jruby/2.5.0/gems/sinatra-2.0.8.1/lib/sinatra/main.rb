module Sinatra
  ParamsConfig = {}

  if ARGV.any?
    require 'optparse'
    parser = OptionParser.new { |op|
      op.on('-p port',   'set the port (default is 4567)')                { |val| ParamsConfig[:port] = Integer(val) }
      op.on('-s server', 'specify rack server/handler (default is thin)') { |val| ParamsConfig[:server] = val }
      op.on('-q',        'turn on quiet mode (default is off)')           {       ParamsConfig[:quiet] = true }
      op.on('-x',        'turn on the mutex lock (default is off)')       {       ParamsConfig[:lock] = true }
      op.on('-e env',    'set the environment (default is development)')  do |val|
        ENV['RACK_ENV'] = val
        ParamsConfig[:environment] = val.to_sym
      end
      op.on('-o addr', "set the host (default is (env == 'development' ? 'localhost' : '0.0.0.0'))") do |val|
        ParamsConfig[:bind] = val
      end
    }
    begin
      parser.parse!(ARGV.dup)
    rescue => evar
      ParamsConfig[:optparse_error] = evar
    end
  end

  require 'sinatra/base'

  class Application < Base

    # we assume that the first file that requires 'sinatra' is the
    # app_file. all other path related options are calculated based
    # on this path by default.
    set :app_file, caller_files.first || $0

    set :run, Proc.new { File.expand_path($0) == File.expand_path(app_file) }

    if run? && ARGV.any?
      error = ParamsConfig.delete(:optparse_error)
      raise error if error
      ParamsConfig.each { |k, v| set k, v }
    end
  end

  remove_const(:ParamsConfig)
  at_exit { Application.run! if $!.nil? && Application.run? }
end

# include would include the module in Object
# extend only extends the `main` object
extend Sinatra::Delegator

class Rack::Builder
  include Sinatra::Delegator
end
