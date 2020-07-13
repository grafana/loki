# frozen_string_literal: true

require 'uri'
require 'stringio'
require_relative '../rack'
require 'cgi/cookie'

module Rack
  # Rack::MockRequest helps testing your Rack application without
  # actually using HTTP.
  #
  # After performing a request on a URL with get/post/put/patch/delete, it
  # returns a MockResponse with useful helper methods for effective
  # testing.
  #
  # You can pass a hash with additional configuration to the
  # get/post/put/patch/delete.
  # <tt>:input</tt>:: A String or IO-like to be used as rack.input.
  # <tt>:fatal</tt>:: Raise a FatalWarning if the app writes to rack.errors.
  # <tt>:lint</tt>:: If true, wrap the application in a Rack::Lint.

  class MockRequest
    class FatalWarning < RuntimeError
    end

    class FatalWarner
      def puts(warning)
        raise FatalWarning, warning
      end

      def write(warning)
        raise FatalWarning, warning
      end

      def flush
      end

      def string
        ""
      end
    end

    DEFAULT_ENV = {
      RACK_VERSION      => Rack::VERSION,
      RACK_INPUT        => StringIO.new,
      RACK_ERRORS       => StringIO.new,
      RACK_MULTITHREAD  => true,
      RACK_MULTIPROCESS => true,
      RACK_RUNONCE      => false,
    }.freeze

    def initialize(app)
      @app = app
    end

    # Make a GET request and return a MockResponse. See #request.
    def get(uri, opts = {})     request(GET, uri, opts)     end
    # Make a POST request and return a MockResponse. See #request.
    def post(uri, opts = {})    request(POST, uri, opts)    end
    # Make a PUT request and return a MockResponse. See #request.
    def put(uri, opts = {})     request(PUT, uri, opts)     end
    # Make a PATCH request and return a MockResponse. See #request.
    def patch(uri, opts = {})   request(PATCH, uri, opts)   end
    # Make a DELETE request and return a MockResponse. See #request.
    def delete(uri, opts = {})  request(DELETE, uri, opts)  end
    # Make a HEAD request and return a MockResponse. See #request.
    def head(uri, opts = {})    request(HEAD, uri, opts)    end
    # Make an OPTIONS request and return a MockResponse. See #request.
    def options(uri, opts = {}) request(OPTIONS, uri, opts) end

    # Make a request using the given request method for the given
    # uri to the rack application and return a MockResponse.
    # Options given are passed to MockRequest.env_for.
    def request(method = GET, uri = "", opts = {})
      env = self.class.env_for(uri, opts.merge(method: method))

      if opts[:lint]
        app = Rack::Lint.new(@app)
      else
        app = @app
      end

      errors = env[RACK_ERRORS]
      status, headers, body = app.call(env)
      MockResponse.new(status, headers, body, errors)
    ensure
      body.close if body.respond_to?(:close)
    end

    # For historical reasons, we're pinning to RFC 2396.
    # URI::Parser = URI::RFC2396_Parser
    def self.parse_uri_rfc2396(uri)
      @parser ||= URI::Parser.new
      @parser.parse(uri)
    end

    # Return the Rack environment used for a request to +uri+.
    # All options that are strings are added to the returned environment.
    # Options:
    # :fatal :: Whether to raise an exception if request outputs to rack.errors
    # :input :: The rack.input to set
    # :method :: The HTTP request method to use
    # :params :: The params to use
    # :script_name :: The SCRIPT_NAME to set
    def self.env_for(uri = "", opts = {})
      uri = parse_uri_rfc2396(uri)
      uri.path = "/#{uri.path}" unless uri.path[0] == ?/

      env = DEFAULT_ENV.dup

      env[REQUEST_METHOD]  = (opts[:method] ? opts[:method].to_s.upcase : GET).b
      env[SERVER_NAME]     = (uri.host || "example.org").b
      env[SERVER_PORT]     = (uri.port ? uri.port.to_s : "80").b
      env[QUERY_STRING]    = (uri.query.to_s).b
      env[PATH_INFO]       = ((!uri.path || uri.path.empty?) ? "/" : uri.path).b
      env[RACK_URL_SCHEME] = (uri.scheme || "http").b
      env[HTTPS]           = (env[RACK_URL_SCHEME] == "https" ? "on" : "off").b

      env[SCRIPT_NAME] = opts[:script_name] || ""

      if opts[:fatal]
        env[RACK_ERRORS] = FatalWarner.new
      else
        env[RACK_ERRORS] = StringIO.new
      end

      if params = opts[:params]
        if env[REQUEST_METHOD] == GET
          params = Utils.parse_nested_query(params) if params.is_a?(String)
          params.update(Utils.parse_nested_query(env[QUERY_STRING]))
          env[QUERY_STRING] = Utils.build_nested_query(params)
        elsif !opts.has_key?(:input)
          opts["CONTENT_TYPE"] = "application/x-www-form-urlencoded"
          if params.is_a?(Hash)
            if data = Rack::Multipart.build_multipart(params)
              opts[:input] = data
              opts["CONTENT_LENGTH"] ||= data.length.to_s
              opts["CONTENT_TYPE"] = "multipart/form-data; boundary=#{Rack::Multipart::MULTIPART_BOUNDARY}"
            else
              opts[:input] = Utils.build_nested_query(params)
            end
          else
            opts[:input] = params
          end
        end
      end

      empty_str = String.new
      opts[:input] ||= empty_str
      if String === opts[:input]
        rack_input = StringIO.new(opts[:input])
      else
        rack_input = opts[:input]
      end

      rack_input.set_encoding(Encoding::BINARY)
      env[RACK_INPUT] = rack_input

      env["CONTENT_LENGTH"] ||= env[RACK_INPUT].size.to_s if env[RACK_INPUT].respond_to?(:size)

      opts.each { |field, value|
        env[field] = value  if String === field
      }

      env
    end
  end

  # Rack::MockResponse provides useful helpers for testing your apps.
  # Usually, you don't create the MockResponse on your own, but use
  # MockRequest.

  class MockResponse < Rack::Response
    class << self
      alias [] new
    end

    # Headers
    attr_reader :original_headers, :cookies

    # Errors
    attr_accessor :errors

    def initialize(status, headers, body, errors = StringIO.new(""))
      @original_headers = headers
      @errors           = errors.string if errors.respond_to?(:string)
      @cookies = parse_cookies_from_header

      super(body, status, headers)

      buffered_body!
    end

    def =~(other)
      body =~ other
    end

    def match(other)
      body.match other
    end

    def body
      # FIXME: apparently users of MockResponse expect the return value of
      # MockResponse#body to be a string.  However, the real response object
      # returns the body as a list.
      #
      # See spec_showstatus.rb:
      #
      #   should "not replace existing messages" do
      #     ...
      #     res.body.should == "foo!"
      #   end
      buffer = String.new

      super.each do |chunk|
        buffer << chunk
      end

      return buffer
    end

    def empty?
      [201, 204, 304].include? status
    end

    def cookie(name)
      cookies.fetch(name, nil)
    end

    private

    def parse_cookies_from_header
      cookies = Hash.new
      if original_headers.has_key? 'Set-Cookie'
        set_cookie_header = original_headers.fetch('Set-Cookie')
        set_cookie_header.split("\n").each do |cookie|
          cookie_name, cookie_filling = cookie.split('=', 2)
          cookie_attributes = identify_cookie_attributes cookie_filling
          parsed_cookie = CGI::Cookie.new(
            'name' => cookie_name.strip,
            'value' => cookie_attributes.fetch('value'),
            'path' => cookie_attributes.fetch('path', nil),
            'domain' => cookie_attributes.fetch('domain', nil),
            'expires' => cookie_attributes.fetch('expires', nil),
            'secure' => cookie_attributes.fetch('secure', false)
          )
          cookies.store(cookie_name, parsed_cookie)
        end
      end
      cookies
    end

    def identify_cookie_attributes(cookie_filling)
      cookie_bits = cookie_filling.split(';')
      cookie_attributes = Hash.new
      cookie_attributes.store('value', cookie_bits[0].strip)
      cookie_bits.each do |bit|
        if bit.include? '='
          cookie_attribute, attribute_value = bit.split('=')
          cookie_attributes.store(cookie_attribute.strip, attribute_value.strip)
          if cookie_attribute.include? 'max-age'
            cookie_attributes.store('expires', Time.now + attribute_value.strip.to_i)
          end
        end
        if bit.include? 'secure'
          cookie_attributes.store('secure', true)
        end
      end
      cookie_attributes
    end

  end
end
