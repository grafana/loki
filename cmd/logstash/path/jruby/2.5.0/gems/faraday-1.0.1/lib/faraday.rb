# frozen_string_literal: true

require 'cgi'
require 'set'
require 'forwardable'
require 'faraday/middleware_registry'
require 'faraday/dependency_loader'

# This is the main namespace for Faraday.
#
# It provides methods to create {Connection} objects, and HTTP-related
# methods to use directly.
#
# @example Helpful class methods for easy usage
#   Faraday.get "http://faraday.com"
#
# @example Helpful class method `.new` to create {Connection} objects.
#   conn = Faraday.new "http://faraday.com"
#   conn.get '/'
#
module Faraday
  VERSION = '1.0.1'
  METHODS_WITH_QUERY = %w[get head delete trace].freeze
  METHODS_WITH_BODY = %w[post put patch].freeze

  class << self
    # The root path that Faraday is being loaded from.
    #
    # This is the root from where the libraries are auto-loaded.
    #
    # @return [String]
    attr_accessor :root_path

    # Gets or sets the path that the Faraday libs are loaded from.
    # @return [String]
    attr_accessor :lib_path

    # @overload default_adapter
    #   Gets the Symbol key identifying a default Adapter to use
    #   for the default {Faraday::Connection}. Defaults to `:net_http`.
    #   @return [Symbol] the default adapter
    # @overload default_adapter=(adapter)
    #   Updates default adapter while resetting {.default_connection}.
    #   @return [Symbol] the new default_adapter.
    attr_reader :default_adapter

    # Documented below, see default_connection
    attr_writer :default_connection

    # Tells Faraday to ignore the environment proxy (http_proxy).
    # Defaults to `false`.
    # @return [Boolean]
    attr_accessor :ignore_env_proxy

    # Initializes a new {Connection}.
    #
    # @param url [String,Hash] The optional String base URL to use as a prefix
    #           for all requests.  Can also be the options Hash. Any of these
    #           values will be set on every request made, unless overridden
    #           for a specific request.
    # @param options [Hash]
    # @option options [String] :url Base URL
    # @option options [Hash] :params Hash of unencoded URI query params.
    # @option options [Hash] :headers Hash of unencoded HTTP headers.
    # @option options [Hash] :request Hash of request options.
    # @option options [Hash] :ssl Hash of SSL options.
    # @option options [Hash] :proxy Hash of Proxy options.
    # @return [Faraday::Connection]
    #
    # @example With an URL argument
    #   Faraday.new 'http://faraday.com'
    #   # => Faraday::Connection to http://faraday.com
    #
    # @example With an URL argument and an options hash
    #   Faraday.new 'http://faraday.com', params: { page: 1 }
    #   # => Faraday::Connection to http://faraday.com?page=1
    #
    # @example With everything in an options hash
    #   Faraday.new url: 'http://faraday.com',
    #               params: { page: 1 }
    #   # => Faraday::Connection to http://faraday.com?page=1
    def new(url = nil, options = {}, &block)
      options = default_connection_options.merge(options)
      Faraday::Connection.new(url, options, &block)
    end

    # @private
    # Internal: Requires internal Faraday libraries.
    #
    # @param libs [Array] one or more relative String names to Faraday classes.
    # @return [void]
    def require_libs(*libs)
      libs.each do |lib|
        require "#{lib_path}/#{lib}"
      end
    end

    alias require_lib require_libs

    # Documented elsewhere, see default_adapter reader
    def default_adapter=(adapter)
      @default_connection = nil
      @default_adapter = adapter
    end

    def respond_to_missing?(symbol, include_private = false)
      default_connection.respond_to?(symbol, include_private) || super
    end

    private

    # Internal: Proxies method calls on the Faraday constant to
    # .default_connection.
    def method_missing(name, *args, &block)
      if default_connection.respond_to?(name)
        default_connection.send(name, *args, &block)
      else
        super
      end
    end
  end

  self.ignore_env_proxy = false
  self.root_path = File.expand_path __dir__
  self.lib_path = File.expand_path 'faraday', __dir__
  self.default_adapter = :net_http

  # @overload default_connection
  #   Gets the default connection used for simple scripts.
  #   @return [Faraday::Connection] a connection configured with
  #   the default_adapter.
  # @overload default_connection=(connection)
  #   @param connection [Faraday::Connection]
  #   Sets the default {Faraday::Connection} for simple scripts that
  #   access the Faraday constant directly, such as
  #   <code>Faraday.get "https://faraday.com"</code>.
  def self.default_connection
    @default_connection ||= Connection.new(default_connection_options)
  end

  # Gets the default connection options used when calling {Faraday#new}.
  #
  # @return [Faraday::ConnectionOptions]
  def self.default_connection_options
    @default_connection_options ||= ConnectionOptions.new
  end

  # Sets the default options used when calling {Faraday#new}.
  #
  # @param options [Hash, Faraday::ConnectionOptions]
  def self.default_connection_options=(options)
    @default_connection = nil
    @default_connection_options = ConnectionOptions.from(options)
  end

  unless defined?(::Faraday::Timer)
    require 'timeout'
    Timer = Timeout
  end

  require_libs 'utils', 'options', 'connection', 'rack_builder', 'parameters',
               'middleware', 'adapter', 'request', 'response', 'error',
               'file_part', 'param_part'

  require_lib 'autoload' unless ENV['FARADAY_NO_AUTOLOAD']
end
