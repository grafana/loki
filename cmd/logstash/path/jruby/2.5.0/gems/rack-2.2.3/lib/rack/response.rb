# frozen_string_literal: true

require 'time'

module Rack
  # Rack::Response provides a convenient interface to create a Rack
  # response.
  #
  # It allows setting of headers and cookies, and provides useful
  # defaults (an OK response with empty headers and body).
  #
  # You can use Response#write to iteratively generate your response,
  # but note that this is buffered by Rack::Response until you call
  # +finish+.  +finish+ however can take a block inside which calls to
  # +write+ are synchronous with the Rack response.
  #
  # Your application's +call+ should end returning Response#finish.
  class Response
    def self.[](status, headers, body)
      self.new(body, status, headers)
    end

    CHUNKED = 'chunked'
    STATUS_WITH_NO_ENTITY_BODY = Utils::STATUS_WITH_NO_ENTITY_BODY

    attr_accessor :length, :status, :body
    attr_reader :headers

    # @deprecated Use {#headers} instead.
    alias header headers

    # Initialize the response object with the specified body, status
    # and headers.
    #
    # @param body [nil, #each, #to_str] the response body.
    # @param status [Integer] the integer status as defined by the
    # HTTP protocol RFCs.
    # @param headers [#each] a list of key-value header pairs which
    # conform to the HTTP protocol RFCs.
    #
    # Providing a body which responds to #to_str is legacy behaviour.
    def initialize(body = nil, status = 200, headers = {})
      @status = status.to_i
      @headers = Utils::HeaderHash[headers]

      @writer = self.method(:append)

      @block = nil

      # Keep track of whether we have expanded the user supplied body.
      if body.nil?
        @body = []
        @buffered = true
        @length = 0
      elsif body.respond_to?(:to_str)
        @body = [body]
        @buffered = true
        @length = body.to_str.bytesize
      else
        @body = body
        @buffered = false
        @length = 0
      end

      yield self if block_given?
    end

    def redirect(target, status = 302)
      self.status = status
      self.location = target
    end

    def chunked?
      CHUNKED == get_header(TRANSFER_ENCODING)
    end

    # Generate a response array consistent with the requirements of the SPEC.
    # @return [Array] a 3-tuple suitable of `[status, headers, body]`
    # which is suitable to be returned from the middleware `#call(env)` method.
    def finish(&block)
      if STATUS_WITH_NO_ENTITY_BODY[status.to_i]
        delete_header CONTENT_TYPE
        delete_header CONTENT_LENGTH
        close
        return [@status, @headers, []]
      else
        if block_given?
          @block = block
          return [@status, @headers, self]
        else
          return [@status, @headers, @body]
        end
      end
    end

    alias to_a finish           # For *response

    def each(&callback)
      @body.each(&callback)
      @buffered = true

      if @block
        @writer = callback
        @block.call(self)
      end
    end

    # Append to body and update Content-Length.
    #
    # NOTE: Do not mix #write and direct #body access!
    #
    def write(chunk)
      buffered_body!

      @writer.call(chunk.to_s)
    end

    def close
      @body.close if @body.respond_to?(:close)
    end

    def empty?
      @block == nil && @body.empty?
    end

    def has_header?(key);   headers.key? key;   end
    def get_header(key);    headers[key];       end
    def set_header(key, v); headers[key] = v;   end
    def delete_header(key); headers.delete key; end

    alias :[] :get_header
    alias :[]= :set_header

    module Helpers
      def invalid?;             status < 100 || status >= 600;        end

      def informational?;       status >= 100 && status < 200;        end
      def successful?;          status >= 200 && status < 300;        end
      def redirection?;         status >= 300 && status < 400;        end
      def client_error?;        status >= 400 && status < 500;        end
      def server_error?;        status >= 500 && status < 600;        end

      def ok?;                  status == 200;                        end
      def created?;             status == 201;                        end
      def accepted?;            status == 202;                        end
      def no_content?;          status == 204;                        end
      def moved_permanently?;   status == 301;                        end
      def bad_request?;         status == 400;                        end
      def unauthorized?;        status == 401;                        end
      def forbidden?;           status == 403;                        end
      def not_found?;           status == 404;                        end
      def method_not_allowed?;  status == 405;                        end
      def precondition_failed?; status == 412;                        end
      def unprocessable?;       status == 422;                        end

      def redirect?;            [301, 302, 303, 307, 308].include? status; end

      def include?(header)
        has_header? header
      end

      # Add a header that may have multiple values.
      #
      # Example:
      #   response.add_header 'Vary', 'Accept-Encoding'
      #   response.add_header 'Vary', 'Cookie'
      #
      #   assert_equal 'Accept-Encoding,Cookie', response.get_header('Vary')
      #
      # http://www.w3.org/Protocols/rfc2616/rfc2616-sec4.html#sec4.2
      def add_header(key, v)
        if v.nil?
          get_header key
        elsif has_header? key
          set_header key, "#{get_header key},#{v}"
        else
          set_header key, v
        end
      end

      # Get the content type of the response.
      def content_type
        get_header CONTENT_TYPE
      end

      # Set the content type of the response.
      def content_type=(content_type)
        set_header CONTENT_TYPE, content_type
      end

      def media_type
        MediaType.type(content_type)
      end

      def media_type_params
        MediaType.params(content_type)
      end

      def content_length
        cl = get_header CONTENT_LENGTH
        cl ? cl.to_i : cl
      end

      def location
        get_header "Location"
      end

      def location=(location)
        set_header "Location", location
      end

      def set_cookie(key, value)
        cookie_header = get_header SET_COOKIE
        set_header SET_COOKIE, ::Rack::Utils.add_cookie_to_header(cookie_header, key, value)
      end

      def delete_cookie(key, value = {})
        set_header SET_COOKIE, ::Rack::Utils.add_remove_cookie_to_header(get_header(SET_COOKIE), key, value)
      end

      def set_cookie_header
        get_header SET_COOKIE
      end

      def set_cookie_header=(v)
        set_header SET_COOKIE, v
      end

      def cache_control
        get_header CACHE_CONTROL
      end

      def cache_control=(v)
        set_header CACHE_CONTROL, v
      end

      # Specifies that the content shouldn't be cached. Overrides `cache!` if already called.
      def do_not_cache!
        set_header CACHE_CONTROL, "no-cache, must-revalidate"
        set_header EXPIRES, Time.now.httpdate
      end

      # Specify that the content should be cached.
      # @param duration [Integer] The number of seconds until the cache expires.
      # @option directive [String] The cache control directive, one of "public", "private", "no-cache" or "no-store".
      def cache!(duration = 3600, directive: "public")
        unless headers[CACHE_CONTROL] =~ /no-cache/
          set_header CACHE_CONTROL, "#{directive}, max-age=#{duration}"
          set_header EXPIRES, (Time.now + duration).httpdate
        end
      end

      def etag
        get_header ETAG
      end

      def etag=(v)
        set_header ETAG, v
      end

    protected

      def buffered_body!
        return if @buffered

        if @body.is_a?(Array)
          # The user supplied body was an array:
          @body = @body.compact
          @body.each do |part|
            @length += part.to_s.bytesize
          end
        else
          # Turn the user supplied body into a buffered array:
          body = @body
          @body = Array.new

          body.each do |part|
            @writer.call(part.to_s)
          end

          body.close if body.respond_to?(:close)
        end

        @buffered = true
      end

      def append(chunk)
        @body << chunk

        unless chunked?
          @length += chunk.bytesize
          set_header(CONTENT_LENGTH, @length.to_s)
        end

        return chunk
      end
    end

    include Helpers

    class Raw
      include Helpers

      attr_reader :headers
      attr_accessor :status

      def initialize(status, headers)
        @status = status
        @headers = headers
      end

      def has_header?(key);   headers.key? key;   end
      def get_header(key);    headers[key];       end
      def set_header(key, v); headers[key] = v;   end
      def delete_header(key); headers.delete key; end
    end
  end
end
