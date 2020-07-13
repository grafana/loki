# -*- encoding: binary -*-
# frozen_string_literal: true

require 'uri'
require 'fileutils'
require 'set'
require 'tempfile'
require 'time'

require_relative 'query_parser'

module Rack
  # Rack::Utils contains a grab-bag of useful methods for writing web
  # applications adopted from all kinds of Ruby libraries.

  module Utils
    (require_relative 'core_ext/regexp'; using ::Rack::RegexpExtensions) if RUBY_VERSION < '2.4'

    ParameterTypeError = QueryParser::ParameterTypeError
    InvalidParameterError = QueryParser::InvalidParameterError
    DEFAULT_SEP = QueryParser::DEFAULT_SEP
    COMMON_SEP = QueryParser::COMMON_SEP
    KeySpaceConstrainedParams = QueryParser::Params

    class << self
      attr_accessor :default_query_parser
    end
    # The default number of bytes to allow parameter keys to take up.
    # This helps prevent a rogue client from flooding a Request.
    self.default_query_parser = QueryParser.make_default(65536, 100)

    module_function

    # URI escapes. (CGI style space to +)
    def escape(s)
      URI.encode_www_form_component(s)
    end

    # Like URI escaping, but with %20 instead of +. Strictly speaking this is
    # true URI escaping.
    def escape_path(s)
      ::URI::DEFAULT_PARSER.escape s
    end

    # Unescapes the **path** component of a URI.  See Rack::Utils.unescape for
    # unescaping query parameters or form components.
    def unescape_path(s)
      ::URI::DEFAULT_PARSER.unescape s
    end

    # Unescapes a URI escaped string with +encoding+. +encoding+ will be the
    # target encoding of the string returned, and it defaults to UTF-8
    def unescape(s, encoding = Encoding::UTF_8)
      URI.decode_www_form_component(s, encoding)
    end

    class << self
      attr_accessor :multipart_part_limit
    end

    # The maximum number of parts a request can contain. Accepting too many part
    # can lead to the server running out of file handles.
    # Set to `0` for no limit.
    self.multipart_part_limit = (ENV['RACK_MULTIPART_PART_LIMIT'] || 128).to_i

    def self.param_depth_limit
      default_query_parser.param_depth_limit
    end

    def self.param_depth_limit=(v)
      self.default_query_parser = self.default_query_parser.new_depth_limit(v)
    end

    def self.key_space_limit
      default_query_parser.key_space_limit
    end

    def self.key_space_limit=(v)
      self.default_query_parser = self.default_query_parser.new_space_limit(v)
    end

    if defined?(Process::CLOCK_MONOTONIC)
      def clock_time
        Process.clock_gettime(Process::CLOCK_MONOTONIC)
      end
    else
      # :nocov:
      def clock_time
        Time.now.to_f
      end
      # :nocov:
    end

    def parse_query(qs, d = nil, &unescaper)
      Rack::Utils.default_query_parser.parse_query(qs, d, &unescaper)
    end

    def parse_nested_query(qs, d = nil)
      Rack::Utils.default_query_parser.parse_nested_query(qs, d)
    end

    def build_query(params)
      params.map { |k, v|
        if v.class == Array
          build_query(v.map { |x| [k, x] })
        else
          v.nil? ? escape(k) : "#{escape(k)}=#{escape(v)}"
        end
      }.join("&")
    end

    def build_nested_query(value, prefix = nil)
      case value
      when Array
        value.map { |v|
          build_nested_query(v, "#{prefix}[]")
        }.join("&")
      when Hash
        value.map { |k, v|
          build_nested_query(v, prefix ? "#{prefix}[#{escape(k)}]" : escape(k))
        }.delete_if(&:empty?).join('&')
      when nil
        prefix
      else
        raise ArgumentError, "value must be a Hash" if prefix.nil?
        "#{prefix}=#{escape(value)}"
      end
    end

    def q_values(q_value_header)
      q_value_header.to_s.split(/\s*,\s*/).map do |part|
        value, parameters = part.split(/\s*;\s*/, 2)
        quality = 1.0
        if parameters && (md = /\Aq=([\d.]+)/.match(parameters))
          quality = md[1].to_f
        end
        [value, quality]
      end
    end

    # Return best accept value to use, based on the algorithm
    # in RFC 2616 Section 14.  If there are multiple best
    # matches (same specificity and quality), the value returned
    # is arbitrary.
    def best_q_match(q_value_header, available_mimes)
      values = q_values(q_value_header)

      matches = values.map do |req_mime, quality|
        match = available_mimes.find { |am| Rack::Mime.match?(am, req_mime) }
        next unless match
        [match, quality]
      end.compact.sort_by do |match, quality|
        (match.split('/', 2).count('*') * -10) + quality
      end.last
      matches && matches.first
    end

    ESCAPE_HTML = {
      "&" => "&amp;",
      "<" => "&lt;",
      ">" => "&gt;",
      "'" => "&#x27;",
      '"' => "&quot;",
      "/" => "&#x2F;"
    }

    ESCAPE_HTML_PATTERN = Regexp.union(*ESCAPE_HTML.keys)

    # Escape ampersands, brackets and quotes to their HTML/XML entities.
    def escape_html(string)
      string.to_s.gsub(ESCAPE_HTML_PATTERN){|c| ESCAPE_HTML[c] }
    end

    def select_best_encoding(available_encodings, accept_encoding)
      # http://www.w3.org/Protocols/rfc2616/rfc2616-sec14.html

      expanded_accept_encoding = []

      accept_encoding.each do |m, q|
        preference = available_encodings.index(m) || available_encodings.size

        if m == "*"
          (available_encodings - accept_encoding.map(&:first)).each do |m2|
            expanded_accept_encoding << [m2, q, preference]
          end
        else
          expanded_accept_encoding << [m, q, preference]
        end
      end

      encoding_candidates = expanded_accept_encoding
        .sort_by { |_, q, p| [-q, p] }
        .map!(&:first)

      unless encoding_candidates.include?("identity")
        encoding_candidates.push("identity")
      end

      expanded_accept_encoding.each do |m, q|
        encoding_candidates.delete(m) if q == 0.0
      end

      (encoding_candidates & available_encodings)[0]
    end

    def parse_cookies(env)
      parse_cookies_header env[HTTP_COOKIE]
    end

    def parse_cookies_header(header)
      # According to RFC 6265:
      # The syntax for cookie headers only supports semicolons
      # User Agent -> Server ==
      # Cookie: SID=31d4d96e407aad42; lang=en-US
      return {} unless header
      header.split(/[;] */n).each_with_object({}) do |cookie, cookies|
        next if cookie.empty?
        key, value = cookie.split('=', 2)
        cookies[key] = (unescape(value) rescue value) unless cookies.key?(key)
      end
    end

    def add_cookie_to_header(header, key, value)
      case value
      when Hash
        domain  = "; domain=#{value[:domain]}"   if value[:domain]
        path    = "; path=#{value[:path]}"       if value[:path]
        max_age = "; max-age=#{value[:max_age]}" if value[:max_age]
        expires = "; expires=#{value[:expires].httpdate}" if value[:expires]
        secure = "; secure"  if value[:secure]
        httponly = "; HttpOnly" if (value.key?(:httponly) ? value[:httponly] : value[:http_only])
        same_site =
          case value[:same_site]
          when false, nil
            nil
          when :none, 'None', :None
            '; SameSite=None'
          when :lax, 'Lax', :Lax
            '; SameSite=Lax'
          when true, :strict, 'Strict', :Strict
            '; SameSite=Strict'
          else
            raise ArgumentError, "Invalid SameSite value: #{value[:same_site].inspect}"
          end
        value = value[:value]
      end
      value = [value] unless Array === value

      cookie = "#{escape(key)}=#{value.map { |v| escape v }.join('&')}#{domain}" \
        "#{path}#{max_age}#{expires}#{secure}#{httponly}#{same_site}"

      case header
      when nil, ''
        cookie
      when String
        [header, cookie].join("\n")
      when Array
        (header + [cookie]).join("\n")
      else
        raise ArgumentError, "Unrecognized cookie header value. Expected String, Array, or nil, got #{header.inspect}"
      end
    end

    def set_cookie_header!(header, key, value)
      header[SET_COOKIE] = add_cookie_to_header(header[SET_COOKIE], key, value)
      nil
    end

    def make_delete_cookie_header(header, key, value)
      case header
      when nil, ''
        cookies = []
      when String
        cookies = header.split("\n")
      when Array
        cookies = header
      end

      key = escape(key)
      domain = value[:domain]
      path = value[:path]
      regexp = if domain
                 if path
                   /\A#{key}=.*(?:domain=#{domain}(?:;|$).*path=#{path}(?:;|$)|path=#{path}(?:;|$).*domain=#{domain}(?:;|$))/
                 else
                   /\A#{key}=.*domain=#{domain}(?:;|$)/
                 end
               elsif path
                 /\A#{key}=.*path=#{path}(?:;|$)/
               else
                 /\A#{key}=/
               end

      cookies.reject! { |cookie| regexp.match? cookie }

      cookies.join("\n")
    end

    def delete_cookie_header!(header, key, value = {})
      header[SET_COOKIE] = add_remove_cookie_to_header(header[SET_COOKIE], key, value)
      nil
    end

    # Adds a cookie that will *remove* a cookie from the client.  Hence the
    # strange method name.
    def add_remove_cookie_to_header(header, key, value = {})
      new_header = make_delete_cookie_header(header, key, value)

      add_cookie_to_header(new_header, key,
                 { value: '', path: nil, domain: nil,
                   max_age: '0',
                   expires: Time.at(0) }.merge(value))

    end

    def rfc2822(time)
      time.rfc2822
    end

    # Modified version of stdlib time.rb Time#rfc2822 to use '%d-%b-%Y' instead
    # of '% %b %Y'.
    # It assumes that the time is in GMT to comply to the RFC 2109.
    #
    # NOTE: I'm not sure the RFC says it requires GMT, but is ambiguous enough
    # that I'm certain someone implemented only that option.
    # Do not use %a and %b from Time.strptime, it would use localized names for
    # weekday and month.
    #
    def rfc2109(time)
      wday = Time::RFC2822_DAY_NAME[time.wday]
      mon = Time::RFC2822_MONTH_NAME[time.mon - 1]
      time.strftime("#{wday}, %d-#{mon}-%Y %H:%M:%S GMT")
    end

    # Parses the "Range:" header, if present, into an array of Range objects.
    # Returns nil if the header is missing or syntactically invalid.
    # Returns an empty array if none of the ranges are satisfiable.
    def byte_ranges(env, size)
      warn "`byte_ranges` is deprecated, please use `get_byte_ranges`" if $VERBOSE
      get_byte_ranges env['HTTP_RANGE'], size
    end

    def get_byte_ranges(http_range, size)
      # See <http://www.w3.org/Protocols/rfc2616/rfc2616-sec14.html#sec14.35>
      return nil unless http_range && http_range =~ /bytes=([^;]+)/
      ranges = []
      $1.split(/,\s*/).each do |range_spec|
        return nil  unless range_spec =~ /(\d*)-(\d*)/
        r0, r1 = $1, $2
        if r0.empty?
          return nil  if r1.empty?
          # suffix-byte-range-spec, represents trailing suffix of file
          r0 = size - r1.to_i
          r0 = 0  if r0 < 0
          r1 = size - 1
        else
          r0 = r0.to_i
          if r1.empty?
            r1 = size - 1
          else
            r1 = r1.to_i
            return nil  if r1 < r0  # backwards range is syntactically invalid
            r1 = size - 1  if r1 >= size
          end
        end
        ranges << (r0..r1)  if r0 <= r1
      end
      ranges
    end

    # Constant time string comparison.
    #
    # NOTE: the values compared should be of fixed length, such as strings
    # that have already been processed by HMAC. This should not be used
    # on variable length plaintext strings because it could leak length info
    # via timing attacks.
    def secure_compare(a, b)
      return false unless a.bytesize == b.bytesize

      l = a.unpack("C*")

      r, i = 0, -1
      b.each_byte { |v| r |= v ^ l[i += 1] }
      r == 0
    end

    # Context allows the use of a compatible middleware at different points
    # in a request handling stack. A compatible middleware must define
    # #context which should take the arguments env and app. The first of which
    # would be the request environment. The second of which would be the rack
    # application that the request would be forwarded to.
    class Context
      attr_reader :for, :app

      def initialize(app_f, app_r)
        raise 'running context does not respond to #context' unless app_f.respond_to? :context
        @for, @app = app_f, app_r
      end

      def call(env)
        @for.context(env, @app)
      end

      def recontext(app)
        self.class.new(@for, app)
      end

      def context(env, app = @app)
        recontext(app).call(env)
      end
    end

    # A case-insensitive Hash that preserves the original case of a
    # header when set.
    #
    # @api private
    class HeaderHash < Hash # :nodoc:
      def self.[](headers)
        if headers.is_a?(HeaderHash) && !headers.frozen?
          return headers
        else
          return self.new(headers)
        end
      end

      def initialize(hash = {})
        super()
        @names = {}
        hash.each { |k, v| self[k] = v }
      end

      # on dup/clone, we need to duplicate @names hash
      def initialize_copy(other)
        super
        @names = other.names.dup
      end

      # on clear, we need to clear @names hash
      def clear
        super
        @names.clear
      end

      def each
        super do |k, v|
          yield(k, v.respond_to?(:to_ary) ? v.to_ary.join("\n") : v)
        end
      end

      def to_hash
        hash = {}
        each { |k, v| hash[k] = v }
        hash
      end

      def [](k)
        super(k) || super(@names[k.downcase])
      end

      def []=(k, v)
        canonical = k.downcase.freeze
        delete k if @names[canonical] && @names[canonical] != k # .delete is expensive, don't invoke it unless necessary
        @names[canonical] = k
        super k, v
      end

      def delete(k)
        canonical = k.downcase
        result = super @names.delete(canonical)
        result
      end

      def include?(k)
        super || @names.include?(k.downcase)
      end

      alias_method :has_key?, :include?
      alias_method :member?, :include?
      alias_method :key?, :include?

      def merge!(other)
        other.each { |k, v| self[k] = v }
        self
      end

      def merge(other)
        hash = dup
        hash.merge! other
      end

      def replace(other)
        clear
        other.each { |k, v| self[k] = v }
        self
      end

      protected
        def names
          @names
        end
    end

    # Every standard HTTP code mapped to the appropriate message.
    # Generated with:
    #   curl -s https://www.iana.org/assignments/http-status-codes/http-status-codes-1.csv | \
    #     ruby -ne 'm = /^(\d{3}),(?!Unassigned|\(Unused\))([^,]+)/.match($_) and \
    #               puts "#{m[1]} => \x27#{m[2].strip}\x27,"'
    HTTP_STATUS_CODES = {
      100 => 'Continue',
      101 => 'Switching Protocols',
      102 => 'Processing',
      103 => 'Early Hints',
      200 => 'OK',
      201 => 'Created',
      202 => 'Accepted',
      203 => 'Non-Authoritative Information',
      204 => 'No Content',
      205 => 'Reset Content',
      206 => 'Partial Content',
      207 => 'Multi-Status',
      208 => 'Already Reported',
      226 => 'IM Used',
      300 => 'Multiple Choices',
      301 => 'Moved Permanently',
      302 => 'Found',
      303 => 'See Other',
      304 => 'Not Modified',
      305 => 'Use Proxy',
      306 => '(Unused)',
      307 => 'Temporary Redirect',
      308 => 'Permanent Redirect',
      400 => 'Bad Request',
      401 => 'Unauthorized',
      402 => 'Payment Required',
      403 => 'Forbidden',
      404 => 'Not Found',
      405 => 'Method Not Allowed',
      406 => 'Not Acceptable',
      407 => 'Proxy Authentication Required',
      408 => 'Request Timeout',
      409 => 'Conflict',
      410 => 'Gone',
      411 => 'Length Required',
      412 => 'Precondition Failed',
      413 => 'Payload Too Large',
      414 => 'URI Too Long',
      415 => 'Unsupported Media Type',
      416 => 'Range Not Satisfiable',
      417 => 'Expectation Failed',
      421 => 'Misdirected Request',
      422 => 'Unprocessable Entity',
      423 => 'Locked',
      424 => 'Failed Dependency',
      425 => 'Too Early',
      426 => 'Upgrade Required',
      428 => 'Precondition Required',
      429 => 'Too Many Requests',
      431 => 'Request Header Fields Too Large',
      451 => 'Unavailable for Legal Reasons',
      500 => 'Internal Server Error',
      501 => 'Not Implemented',
      502 => 'Bad Gateway',
      503 => 'Service Unavailable',
      504 => 'Gateway Timeout',
      505 => 'HTTP Version Not Supported',
      506 => 'Variant Also Negotiates',
      507 => 'Insufficient Storage',
      508 => 'Loop Detected',
      509 => 'Bandwidth Limit Exceeded',
      510 => 'Not Extended',
      511 => 'Network Authentication Required'
    }

    # Responses with HTTP status codes that should not have an entity body
    STATUS_WITH_NO_ENTITY_BODY = Hash[((100..199).to_a << 204 << 304).product([true])]

    SYMBOL_TO_STATUS_CODE = Hash[*HTTP_STATUS_CODES.map { |code, message|
      [message.downcase.gsub(/\s|-|'/, '_').to_sym, code]
    }.flatten]

    def status_code(status)
      if status.is_a?(Symbol)
        SYMBOL_TO_STATUS_CODE.fetch(status) { raise ArgumentError, "Unrecognized status code #{status.inspect}" }
      else
        status.to_i
      end
    end

    PATH_SEPS = Regexp.union(*[::File::SEPARATOR, ::File::ALT_SEPARATOR].compact)

    def clean_path_info(path_info)
      parts = path_info.split PATH_SEPS

      clean = []

      parts.each do |part|
        next if part.empty? || part == '.'
        part == '..' ? clean.pop : clean << part
      end

      clean_path = clean.join(::File::SEPARATOR)
      clean_path.prepend("/") if parts.empty? || parts.first.empty?
      clean_path
    end

    NULL_BYTE = "\0"

    def valid_path?(path)
      path.valid_encoding? && !path.include?(NULL_BYTE)
    end

  end
end
