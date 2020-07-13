# frozen_string_literal: true

module Faraday
  # ProxyOptions contains the configurable properties for the proxy
  # configuration used when making an HTTP request.
  class ProxyOptions < Options.new(:uri, :user, :password)
    extend Forwardable
    def_delegators :uri, :scheme, :scheme=, :host, :host=, :port, :port=,
                   :path, :path=

    def self.from(value)
      case value
      when String
        value = { uri: Utils.URI(value) }
      when URI
        value = { uri: value }
      when Hash, Options
        if (uri = value.delete(:uri))
          value[:uri] = Utils.URI(uri)
        end
      end
      super(value)
    end

    memoized(:user) { uri&.user && Utils.unescape(uri.user) }
    memoized(:password) { uri&.password && Utils.unescape(uri.password) }
  end
end
