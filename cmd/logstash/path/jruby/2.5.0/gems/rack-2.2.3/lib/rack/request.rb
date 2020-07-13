# frozen_string_literal: true

module Rack
  # Rack::Request provides a convenient interface to a Rack
  # environment.  It is stateless, the environment +env+ passed to the
  # constructor will be directly modified.
  #
  #   req = Rack::Request.new(env)
  #   req.post?
  #   req.params["data"]

  class Request
    (require_relative 'core_ext/regexp'; using ::Rack::RegexpExtensions) if RUBY_VERSION < '2.4'

    class << self
      attr_accessor :ip_filter
    end

    self.ip_filter = lambda { |ip| /\A127\.0\.0\.1\Z|\A(10|172\.(1[6-9]|2[0-9]|30|31)|192\.168)\.|\A::1\Z|\Afd[0-9a-f]{2}:.+|\Alocalhost\Z|\Aunix\Z|\Aunix:/i.match?(ip) }
    ALLOWED_SCHEMES = %w(https http).freeze
    SCHEME_WHITELIST = ALLOWED_SCHEMES
    if Object.respond_to?(:deprecate_constant)
      deprecate_constant :SCHEME_WHITELIST
    end

    def initialize(env)
      @params = nil
      super(env)
    end

    def params
      @params ||= super
    end

    def update_param(k, v)
      super
      @params = nil
    end

    def delete_param(k)
      v = super
      @params = nil
      v
    end

    module Env
      # The environment of the request.
      attr_reader :env

      def initialize(env)
        @env = env
        super()
      end

      # Predicate method to test to see if `name` has been set as request
      # specific data
      def has_header?(name)
        @env.key? name
      end

      # Get a request specific value for `name`.
      def get_header(name)
        @env[name]
      end

      # If a block is given, it yields to the block if the value hasn't been set
      # on the request.
      def fetch_header(name, &block)
        @env.fetch(name, &block)
      end

      # Loops through each key / value pair in the request specific data.
      def each_header(&block)
        @env.each(&block)
      end

      # Set a request specific value for `name` to `v`
      def set_header(name, v)
        @env[name] = v
      end

      # Add a header that may have multiple values.
      #
      # Example:
      #   request.add_header 'Accept', 'image/png'
      #   request.add_header 'Accept', '*/*'
      #
      #   assert_equal 'image/png,*/*', request.get_header('Accept')
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

      # Delete a request specific value for `name`.
      def delete_header(name)
        @env.delete name
      end

      def initialize_copy(other)
        @env = other.env.dup
      end
    end

    module Helpers
      # The set of form-data media-types. Requests that do not indicate
      # one of the media types present in this list will not be eligible
      # for form-data / param parsing.
      FORM_DATA_MEDIA_TYPES = [
        'application/x-www-form-urlencoded',
        'multipart/form-data'
      ]

      # The set of media-types. Requests that do not indicate
      # one of the media types present in this list will not be eligible
      # for param parsing like soap attachments or generic multiparts
      PARSEABLE_DATA_MEDIA_TYPES = [
        'multipart/related',
        'multipart/mixed'
      ]

      # Default ports depending on scheme. Used to decide whether or not
      # to include the port in a generated URI.
      DEFAULT_PORTS = { 'http' => 80, 'https' => 443, 'coffee' => 80 }

      # The address of the client which connected to the proxy.
      HTTP_X_FORWARDED_FOR = 'HTTP_X_FORWARDED_FOR'

      # The contents of the host/:authority header sent to the proxy.
      HTTP_X_FORWARDED_HOST = 'HTTP_X_FORWARDED_HOST'

      # The value of the scheme sent to the proxy.
      HTTP_X_FORWARDED_SCHEME = 'HTTP_X_FORWARDED_SCHEME'

      # The protocol used to connect to the proxy.
      HTTP_X_FORWARDED_PROTO = 'HTTP_X_FORWARDED_PROTO'

      # The port used to connect to the proxy.
      HTTP_X_FORWARDED_PORT = 'HTTP_X_FORWARDED_PORT'

      # Another way for specifing https scheme was used.
      HTTP_X_FORWARDED_SSL = 'HTTP_X_FORWARDED_SSL'

      def body;            get_header(RACK_INPUT)                         end
      def script_name;     get_header(SCRIPT_NAME).to_s                   end
      def script_name=(s); set_header(SCRIPT_NAME, s.to_s)                end

      def path_info;       get_header(PATH_INFO).to_s                     end
      def path_info=(s);   set_header(PATH_INFO, s.to_s)                  end

      def request_method;  get_header(REQUEST_METHOD)                     end
      def query_string;    get_header(QUERY_STRING).to_s                  end
      def content_length;  get_header('CONTENT_LENGTH')                   end
      def logger;          get_header(RACK_LOGGER)                        end
      def user_agent;      get_header('HTTP_USER_AGENT')                  end
      def multithread?;    get_header(RACK_MULTITHREAD)                   end

      # the referer of the client
      def referer;         get_header('HTTP_REFERER')                     end
      alias referrer referer

      def session
        fetch_header(RACK_SESSION) do |k|
          set_header RACK_SESSION, default_session
        end
      end

      def session_options
        fetch_header(RACK_SESSION_OPTIONS) do |k|
          set_header RACK_SESSION_OPTIONS, {}
        end
      end

      # Checks the HTTP request method (or verb) to see if it was of type DELETE
      def delete?;  request_method == DELETE  end

      # Checks the HTTP request method (or verb) to see if it was of type GET
      def get?;     request_method == GET     end

      # Checks the HTTP request method (or verb) to see if it was of type HEAD
      def head?;    request_method == HEAD    end

      # Checks the HTTP request method (or verb) to see if it was of type OPTIONS
      def options?; request_method == OPTIONS end

      # Checks the HTTP request method (or verb) to see if it was of type LINK
      def link?;    request_method == LINK    end

      # Checks the HTTP request method (or verb) to see if it was of type PATCH
      def patch?;   request_method == PATCH   end

      # Checks the HTTP request method (or verb) to see if it was of type POST
      def post?;    request_method == POST    end

      # Checks the HTTP request method (or verb) to see if it was of type PUT
      def put?;     request_method == PUT     end

      # Checks the HTTP request method (or verb) to see if it was of type TRACE
      def trace?;   request_method == TRACE   end

      # Checks the HTTP request method (or verb) to see if it was of type UNLINK
      def unlink?;  request_method == UNLINK  end

      def scheme
        if get_header(HTTPS) == 'on'
          'https'
        elsif get_header(HTTP_X_FORWARDED_SSL) == 'on'
          'https'
        elsif forwarded_scheme
          forwarded_scheme
        else
          get_header(RACK_URL_SCHEME)
        end
      end

      # The authority of the incoming request as defined by RFC3976.
      # https://tools.ietf.org/html/rfc3986#section-3.2
      #
      # In HTTP/1, this is the `host` header.
      # In HTTP/2, this is the `:authority` pseudo-header.
      def authority
        forwarded_authority || host_authority || server_authority
      end

      # The authority as defined by the `SERVER_NAME` and `SERVER_PORT`
      # variables.
      def server_authority
        host = self.server_name
        port = self.server_port

        if host
          if port
            "#{host}:#{port}"
          else
            host
          end
        end
      end

      def server_name
        get_header(SERVER_NAME)
      end

      def server_port
        if port = get_header(SERVER_PORT)
          Integer(port)
        end
      end

      def cookies
        hash = fetch_header(RACK_REQUEST_COOKIE_HASH) do |key|
          set_header(key, {})
        end

        string = get_header(HTTP_COOKIE)

        unless string == get_header(RACK_REQUEST_COOKIE_STRING)
          hash.replace Utils.parse_cookies_header(string)
          set_header(RACK_REQUEST_COOKIE_STRING, string)
        end

        hash
      end

      def content_type
        content_type = get_header('CONTENT_TYPE')
        content_type.nil? || content_type.empty? ? nil : content_type
      end

      def xhr?
        get_header("HTTP_X_REQUESTED_WITH") == "XMLHttpRequest"
      end

      # The `HTTP_HOST` header.
      def host_authority
        get_header(HTTP_HOST)
      end

      def host_with_port(authority = self.authority)
        host, _, port = split_authority(authority)

        if port == DEFAULT_PORTS[self.scheme]
          host
        else
          authority
        end
      end

      # Returns a formatted host, suitable for being used in a URI.
      def host
        split_authority(self.authority)[0]
      end

      # Returns an address suitable for being to resolve to an address.
      # In the case of a domain name or IPv4 address, the result is the same
      # as +host+. In the case of IPv6 or future address formats, the square
      # brackets are removed.
      def hostname
        split_authority(self.authority)[1]
      end

      def port
        if authority = self.authority
          _, _, port = split_authority(self.authority)

          if port
            return port
          end
        end

        if forwarded_port = self.forwarded_port
          return forwarded_port.first
        end

        if scheme = self.scheme
          if port = DEFAULT_PORTS[self.scheme]
            return port
          end
        end

        self.server_port
      end

      def forwarded_for
        if value = get_header(HTTP_X_FORWARDED_FOR)
          split_header(value).map do |authority|
            split_authority(wrap_ipv6(authority))[1]
          end
        end
      end

      def forwarded_port
        if value = get_header(HTTP_X_FORWARDED_PORT)
          split_header(value).map(&:to_i)
        end
      end

      def forwarded_authority
        if value = get_header(HTTP_X_FORWARDED_HOST)
          wrap_ipv6(split_header(value).first)
        end
      end

      def ssl?
        scheme == 'https' || scheme == 'wss'
      end

      def ip
        remote_addresses = split_header(get_header('REMOTE_ADDR'))
        external_addresses = reject_trusted_ip_addresses(remote_addresses)

        unless external_addresses.empty?
          return external_addresses.first
        end

        if forwarded_for = self.forwarded_for
          unless forwarded_for.empty?
            # The forwarded for addresses are ordered: client, proxy1, proxy2.
            # So we reject all the trusted addresses (proxy*) and return the
            # last client. Or if we trust everyone, we just return the first
            # address.
            return reject_trusted_ip_addresses(forwarded_for).last || forwarded_for.first
          end
        end

        # If all the addresses are trusted, and we aren't forwarded, just return
        # the first remote address, which represents the source of the request.
        remote_addresses.first
      end

      # The media type (type/subtype) portion of the CONTENT_TYPE header
      # without any media type parameters. e.g., when CONTENT_TYPE is
      # "text/plain;charset=utf-8", the media-type is "text/plain".
      #
      # For more information on the use of media types in HTTP, see:
      # http://www.w3.org/Protocols/rfc2616/rfc2616-sec3.html#sec3.7
      def media_type
        MediaType.type(content_type)
      end

      # The media type parameters provided in CONTENT_TYPE as a Hash, or
      # an empty Hash if no CONTENT_TYPE or media-type parameters were
      # provided.  e.g., when the CONTENT_TYPE is "text/plain;charset=utf-8",
      # this method responds with the following Hash:
      #   { 'charset' => 'utf-8' }
      def media_type_params
        MediaType.params(content_type)
      end

      # The character set of the request body if a "charset" media type
      # parameter was given, or nil if no "charset" was specified. Note
      # that, per RFC2616, text/* media types that specify no explicit
      # charset are to be considered ISO-8859-1.
      def content_charset
        media_type_params['charset']
      end

      # Determine whether the request body contains form-data by checking
      # the request Content-Type for one of the media-types:
      # "application/x-www-form-urlencoded" or "multipart/form-data". The
      # list of form-data media types can be modified through the
      # +FORM_DATA_MEDIA_TYPES+ array.
      #
      # A request body is also assumed to contain form-data when no
      # Content-Type header is provided and the request_method is POST.
      def form_data?
        type = media_type
        meth = get_header(RACK_METHODOVERRIDE_ORIGINAL_METHOD) || get_header(REQUEST_METHOD)

        (meth == POST && type.nil?) || FORM_DATA_MEDIA_TYPES.include?(type)
      end

      # Determine whether the request body contains data by checking
      # the request media_type against registered parse-data media-types
      def parseable_data?
        PARSEABLE_DATA_MEDIA_TYPES.include?(media_type)
      end

      # Returns the data received in the query string.
      def GET
        if get_header(RACK_REQUEST_QUERY_STRING) == query_string
          get_header(RACK_REQUEST_QUERY_HASH)
        else
          query_hash = parse_query(query_string, '&;')
          set_header(RACK_REQUEST_QUERY_STRING, query_string)
          set_header(RACK_REQUEST_QUERY_HASH, query_hash)
        end
      end

      # Returns the data received in the request body.
      #
      # This method support both application/x-www-form-urlencoded and
      # multipart/form-data.
      def POST
        if get_header(RACK_INPUT).nil?
          raise "Missing rack.input"
        elsif get_header(RACK_REQUEST_FORM_INPUT) == get_header(RACK_INPUT)
          get_header(RACK_REQUEST_FORM_HASH)
        elsif form_data? || parseable_data?
          unless set_header(RACK_REQUEST_FORM_HASH, parse_multipart)
            form_vars = get_header(RACK_INPUT).read

            # Fix for Safari Ajax postings that always append \0
            # form_vars.sub!(/\0\z/, '') # performance replacement:
            form_vars.slice!(-1) if form_vars.end_with?("\0")

            set_header RACK_REQUEST_FORM_VARS, form_vars
            set_header RACK_REQUEST_FORM_HASH, parse_query(form_vars, '&')

            get_header(RACK_INPUT).rewind
          end
          set_header RACK_REQUEST_FORM_INPUT, get_header(RACK_INPUT)
          get_header RACK_REQUEST_FORM_HASH
        else
          {}
        end
      end

      # The union of GET and POST data.
      #
      # Note that modifications will not be persisted in the env. Use update_param or delete_param if you want to destructively modify params.
      def params
        self.GET.merge(self.POST)
      end

      # Destructively update a parameter, whether it's in GET and/or POST. Returns nil.
      #
      # The parameter is updated wherever it was previous defined, so GET, POST, or both. If it wasn't previously defined, it's inserted into GET.
      #
      # <tt>env['rack.input']</tt> is not touched.
      def update_param(k, v)
        found = false
        if self.GET.has_key?(k)
          found = true
          self.GET[k] = v
        end
        if self.POST.has_key?(k)
          found = true
          self.POST[k] = v
        end
        unless found
          self.GET[k] = v
        end
      end

      # Destructively delete a parameter, whether it's in GET or POST. Returns the value of the deleted parameter.
      #
      # If the parameter is in both GET and POST, the POST value takes precedence since that's how #params works.
      #
      # <tt>env['rack.input']</tt> is not touched.
      def delete_param(k)
        post_value, get_value = self.POST.delete(k), self.GET.delete(k)
        post_value || get_value
      end

      def base_url
        "#{scheme}://#{host_with_port}"
      end

      # Tries to return a remake of the original request URL as a string.
      def url
        base_url + fullpath
      end

      def path
        script_name + path_info
      end

      def fullpath
        query_string.empty? ? path : "#{path}?#{query_string}"
      end

      def accept_encoding
        parse_http_accept_header(get_header("HTTP_ACCEPT_ENCODING"))
      end

      def accept_language
        parse_http_accept_header(get_header("HTTP_ACCEPT_LANGUAGE"))
      end

      def trusted_proxy?(ip)
        Rack::Request.ip_filter.call(ip)
      end

      # shortcut for <tt>request.params[key]</tt>
      def [](key)
        if $VERBOSE
          warn("Request#[] is deprecated and will be removed in a future version of Rack. Please use request.params[] instead")
        end

        params[key.to_s]
      end

      # shortcut for <tt>request.params[key] = value</tt>
      #
      # Note that modifications will not be persisted in the env. Use update_param or delete_param if you want to destructively modify params.
      def []=(key, value)
        if $VERBOSE
          warn("Request#[]= is deprecated and will be removed in a future version of Rack. Please use request.params[]= instead")
        end

        params[key.to_s] = value
      end

      # like Hash#values_at
      def values_at(*keys)
        keys.map { |key| params[key] }
      end

      private

      def default_session; {}; end

      # Assist with compatibility when processing `X-Forwarded-For`.
      def wrap_ipv6(host)
        # Even thought IPv6 addresses should be wrapped in square brackets,
        # sometimes this is not done in various legacy/underspecified headers.
        # So we try to fix this situation for compatibility reasons.

        # Try to detect IPv6 addresses which aren't escaped yet:
        if !host.start_with?('[') && host.count(':') > 1
          "[#{host}]"
        else
          host
        end
      end

      def parse_http_accept_header(header)
        header.to_s.split(/\s*,\s*/).map do |part|
          attribute, parameters = part.split(/\s*;\s*/, 2)
          quality = 1.0
          if parameters and /\Aq=([\d.]+)/ =~ parameters
            quality = $1.to_f
          end
          [attribute, quality]
        end
      end

      def query_parser
        Utils.default_query_parser
      end

      def parse_query(qs, d = '&')
        query_parser.parse_nested_query(qs, d)
      end

      def parse_multipart
        Rack::Multipart.extract_multipart(self, query_parser)
      end

      def split_header(value)
        value ? value.strip.split(/[,\s]+/) : []
      end

      AUTHORITY = /^
        # The host:
        (?<host>
          # An IPv6 address:
          (\[(?<ip6>.*)\])
          |
          # An IPv4 address:
          (?<ip4>[\d\.]+)
          |
          # A hostname:
          (?<name>[a-zA-Z0-9\.\-]+)
        )
        # The optional port:
        (:(?<port>\d+))?
      $/x

      private_constant :AUTHORITY

      def split_authority(authority)
        if match = AUTHORITY.match(authority)
          if address = match[:ip6]
            return match[:host], address, match[:port]&.to_i
          else
            return match[:host], match[:host], match[:port]&.to_i
          end
        end

        # Give up!
        return authority, authority, nil
      end

      def reject_trusted_ip_addresses(ip_addresses)
        ip_addresses.reject { |ip| trusted_proxy?(ip) }
      end

      def forwarded_scheme
        allowed_scheme(get_header(HTTP_X_FORWARDED_SCHEME)) ||
        allowed_scheme(extract_proto_header(get_header(HTTP_X_FORWARDED_PROTO)))
      end

      def allowed_scheme(header)
        header if ALLOWED_SCHEMES.include?(header)
      end

      def extract_proto_header(header)
        if header
          if (comma_index = header.index(','))
            header[0, comma_index]
          else
            header
          end
        end
      end
    end

    include Env
    include Helpers
  end
end
