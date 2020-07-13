# frozen_string_literal: true

module Rack
  # Rack::CommonLogger forwards every request to the given +app+, and
  # logs a line in the
  # {Apache common log format}[http://httpd.apache.org/docs/1.3/logs.html#common]
  # to the configured logger.
  class CommonLogger
    # Common Log Format: http://httpd.apache.org/docs/1.3/logs.html#common
    #
    #   lilith.local - - [07/Aug/2006 23:58:02 -0400] "GET / HTTP/1.1" 500 -
    #
    #   %{%s - %s [%s] "%s %s%s %s" %d %s\n} %
    #
    # The actual format is slightly different than the above due to the
    # separation of SCRIPT_NAME and PATH_INFO, and because the elapsed
    # time in seconds is included at the end.
    FORMAT = %{%s - %s [%s] "%s %s%s%s %s" %d %s %0.4f\n}

    # +logger+ can be any object that supports the +write+ or +<<+ methods,
    # which includes the standard library Logger.  These methods are called
    # with a single string argument, the log message.
    # If +logger+ is nil, CommonLogger will fall back <tt>env['rack.errors']</tt>.
    def initialize(app, logger = nil)
      @app = app
      @logger = logger
    end

    # Log all requests in common_log format after a response has been
    # returned.  Note that if the app raises an exception, the request
    # will not be logged, so if exception handling middleware are used,
    # they should be loaded after this middleware.  Additionally, because
    # the logging happens after the request body has been fully sent, any
    # exceptions raised during the sending of the response body will
    # cause the request not to be logged.
    def call(env)
      began_at = Utils.clock_time
      status, headers, body = @app.call(env)
      headers = Utils::HeaderHash[headers]
      body = BodyProxy.new(body) { log(env, status, headers, began_at) }
      [status, headers, body]
    end

    private

    # Log the request to the configured logger.
    def log(env, status, header, began_at)
      length = extract_content_length(header)

      msg = FORMAT % [
        env['HTTP_X_FORWARDED_FOR'] || env["REMOTE_ADDR"] || "-",
        env["REMOTE_USER"] || "-",
        Time.now.strftime("%d/%b/%Y:%H:%M:%S %z"),
        env[REQUEST_METHOD],
        env[SCRIPT_NAME],
        env[PATH_INFO],
        env[QUERY_STRING].empty? ? "" : "?#{env[QUERY_STRING]}",
        env[SERVER_PROTOCOL],
        status.to_s[0..3],
        length,
        Utils.clock_time - began_at ]

      logger = @logger || env[RACK_ERRORS]
      # Standard library logger doesn't support write but it supports << which actually
      # calls to write on the log device without formatting
      if logger.respond_to?(:write)
        logger.write(msg)
      else
        logger << msg
      end
    end

    # Attempt to determine the content length for the response to
    # include it in the logged data.
    def extract_content_length(headers)
      value = headers[CONTENT_LENGTH]
      !value || value.to_s == '0' ? '-' : value
    end
  end
end
