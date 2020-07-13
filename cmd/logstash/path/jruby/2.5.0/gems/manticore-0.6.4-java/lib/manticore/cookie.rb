module Manticore
  # @!attribute [r] comment
  #   @return [String] Returns the comment describing the purpose of this cookie, or nil  if no such comment has been defined.
  # @!attribute [r] comment_url
  #   @return [String] If a user agent (web browser) presents this cookie to a user, the cookie's purpose will be described by the information at this URL.
  # @!attribute [r] domain
  #   @return [String] Returns domain attribute of the cookie.
  # @!attribute [r] expiry_date
  #   @return [Time] Returns the expiration Date of the cookie, or nil if none exists.
  # @!attribute [r] name
  #   @return [String] Returns the name of the cookie.
  # @!attribute [r] path
  #   @return [String] Returns the path attribute of the cookie.
  # @!attribute [r] ports
  #   @return [Array<Integer>] Returns the ports attribute of the cookie.
  # @!attribute [r] value
  #   @return [Array<Integer>] Returns the cookie value
  # @!attribute [r] spec_version
  #   @return [Integer] Returns the version of the cookie specification to which this cookie conforms.
  class Cookie
    # @private
    # Create a Manticore::Cookie wrapper from a org.apache.http.cookie.Cookie
    def self.from_java(cookie)
      if cookie.get_expiry_date
        expiry = Time.at(cookie.get_expiry_date / 1000)
      end

      new(
        comment: cookie.get_comment,
        comment_url: cookie.getCommentURL,
        domain: cookie.get_domain,
        expires: expiry,
        name: cookie.get_name,
        path: cookie.get_path,
        ports: cookie.get_ports.to_a,
        value: cookie.get_value,
        secure: cookie.is_secure,
        persistent: cookie.is_persistent,
        spec_version: cookie.get_version,
      )
    end

    def self.from_set_cookie(value)
      opts = {name: nil, value: nil}
      value.split(";").each do |part|
        k, v = part.split("=", 2).map(&:strip)

        if opts[:name].nil?
          opts[:name] = k
          opts[:value] = v
        end

        case k.downcase
        when "domain", "path"
          opts[k.to_sym] = v
        when "secure"
          opts[:secure] = true
        end
      end
      Manticore::Cookie.new opts
    end

    attr_reader :comment, :comment_url, :domain, :expires, :name, :path, :ports, :value, :spec_version

    def initialize(args)
      @comment = args.fetch(:comment, nil)
      @comment_url = args.fetch(:comment_url, nil)
      @domain = args.fetch(:domain, nil)
      @expires = args.fetch(:expires, nil)
      @name = args.fetch(:name, nil)
      @path = args.fetch(:path, nil)
      @ports = args.fetch(:ports, nil)
      @value = args.fetch(:value, nil)
      @secure = args.fetch(:secure, nil)
      @persistent = args.fetch(:persistent, nil)
      @spec_version = args.fetch(:spec_version, nil)
    end

    # @param  date [Time] Time to compare against
    #
    # @return [Boolean] Whether this cookie is expired at the comparison date
    def expired?(date = Time.now)
      @expiry_date > date
    end

    # Whether this is a HTTPS-only cookie
    #
    # @return [Boolean]
    def secure?
      @secure
    end

    # Whether this is a persistent (session-only) cookie
    #
    # @return [Boolean]
    def persistent?
      @persistent
    end

    # Whether this is a session-only cookie
    #
    # @return [Boolean]
    def session?
      !@persistent
    end
  end
end
