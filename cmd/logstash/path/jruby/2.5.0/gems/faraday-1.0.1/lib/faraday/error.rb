# frozen_string_literal: true

# Faraday namespace.
module Faraday
  # Faraday error base class.
  class Error < StandardError
    attr_reader :response, :wrapped_exception

    def initialize(exc, response = nil)
      @wrapped_exception = nil unless defined?(@wrapped_exception)
      @response = nil unless defined?(@response)
      super(exc_msg_and_response!(exc, response))
    end

    def backtrace
      if @wrapped_exception
        @wrapped_exception.backtrace
      else
        super
      end
    end

    def inspect
      inner = +''
      inner << " wrapped=#{@wrapped_exception.inspect}" if @wrapped_exception
      inner << " response=#{@response.inspect}" if @response
      inner << " #{super}" if inner.empty?
      %(#<#{self.class}#{inner}>)
    end

    protected

    # Pulls out potential parent exception and response hash, storing them in
    # instance variables.
    # exc      - Either an Exception, a string message, or a response hash.
    # response - Hash
    #              :status  - Optional integer HTTP response status
    #              :headers - String key/value hash of HTTP response header
    #                         values.
    #              :body    - Optional string HTTP response body.
    #
    # If a subclass has to call this, then it should pass a string message
    # to `super`. See NilStatusError.
    def exc_msg_and_response!(exc, response = nil)
      if @response.nil? && @wrapped_exception.nil?
        @wrapped_exception, msg, @response = exc_msg_and_response(exc, response)
        return msg
      end

      exc.to_s
    end

    # Pulls out potential parent exception and response hash.
    def exc_msg_and_response(exc, response = nil)
      return [exc, exc.message, response] if exc.respond_to?(:backtrace)

      return [nil, "the server responded with status #{exc[:status]}", exc] \
        if exc.respond_to?(:each_key)

      [nil, exc.to_s, response]
    end
  end

  # Faraday client error class. Represents 4xx status responses.
  class ClientError < Error
  end

  # Raised by Faraday::Response::RaiseError in case of a 400 response.
  class BadRequestError < ClientError
  end

  # Raised by Faraday::Response::RaiseError in case of a 401 response.
  class UnauthorizedError < ClientError
  end

  # Raised by Faraday::Response::RaiseError in case of a 403 response.
  class ForbiddenError < ClientError
  end

  # Raised by Faraday::Response::RaiseError in case of a 404 response.
  class ResourceNotFound < ClientError
  end

  # Raised by Faraday::Response::RaiseError in case of a 407 response.
  class ProxyAuthError < ClientError
  end

  # Raised by Faraday::Response::RaiseError in case of a 409 response.
  class ConflictError < ClientError
  end

  # Raised by Faraday::Response::RaiseError in case of a 422 response.
  class UnprocessableEntityError < ClientError
  end

  # Faraday server error class. Represents 5xx status responses.
  class ServerError < Error
  end

  # A unified client error for timeouts.
  class TimeoutError < ServerError
    def initialize(exc = 'timeout', response = nil)
      super(exc, response)
    end
  end

  # Raised by Faraday::Response::RaiseError in case of a nil status in response.
  class NilStatusError < ServerError
    def initialize(exc, response = nil)
      exc_msg_and_response!(exc, response)
      super('http status could not be derived from the server response')
    end
  end

  # A unified error for failed connections.
  class ConnectionFailed < Error
  end

  # A unified client error for SSL errors.
  class SSLError < Error
  end

  # Raised by FaradayMiddleware::ResponseMiddleware
  class ParsingError < Error
  end

  # Exception used to control the Retry middleware.
  #
  # @see Faraday::Request::Retry
  class RetriableResponse < Error
  end
end
