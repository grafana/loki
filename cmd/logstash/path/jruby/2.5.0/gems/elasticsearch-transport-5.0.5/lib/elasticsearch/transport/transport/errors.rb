module Elasticsearch
  module Transport
    module Transport

      # Generic client error
      #
      class Error < StandardError; end

      # Reloading connections timeout (1 sec by default)
      #
      class SnifferTimeoutError < Timeout::Error; end

      # Elasticsearch server error (HTTP status 5xx)
      #
      class ServerError < Error; end

      module Errors; end

      HTTP_STATUSES = {
        300 => 'MultipleChoices',
        301 => 'MovedPermanently',
        302 => 'Found',
        303 => 'SeeOther',
        304 => 'NotModified',
        305 => 'UseProxy',
        307 => 'TemporaryRedirect',
        308 => 'PermanentRedirect',

        400 => 'BadRequest',
        401 => 'Unauthorized',
        402 => 'PaymentRequired',
        403 => 'Forbidden',
        404 => 'NotFound',
        405 => 'MethodNotAllowed',
        406 => 'NotAcceptable',
        407 => 'ProxyAuthenticationRequired',
        408 => 'RequestTimeout',
        409 => 'Conflict',
        410 => 'Gone',
        411 => 'LengthRequired',
        412 => 'PreconditionFailed',
        413 => 'RequestEntityTooLarge',
        414 => 'RequestURITooLong',
        415 => 'UnsupportedMediaType',
        416 => 'RequestedRangeNotSatisfiable',
        417 => 'ExpectationFailed',
        418 => 'ImATeapot',
        421 => 'TooManyConnectionsFromThisIP',
        426 => 'UpgradeRequired',
        450 => 'BlockedByWindowsParentalControls',
        494 => 'RequestHeaderTooLarge',
        497 => 'HTTPToHTTPS',
        499 => 'ClientClosedRequest',

        500 => 'InternalServerError',
        501 => 'NotImplemented',
        502 => 'BadGateway',
        503 => 'ServiceUnavailable',
        504 => 'GatewayTimeout',
        505 => 'HTTPVersionNotSupported',
        506 => 'VariantAlsoNegotiates',
        510 => 'NotExtended'
      }

      ERRORS = HTTP_STATUSES.inject({}) do |sum, error|
        status, name = error
        sum[status] = Errors.const_set name, Class.new(ServerError)
        sum
      end

    end
  end
end
