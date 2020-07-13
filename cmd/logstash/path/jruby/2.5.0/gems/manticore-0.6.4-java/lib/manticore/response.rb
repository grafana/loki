module Manticore
  # Implementation of {http://hc.apache.org/httpcomponents-client-ga/httpclient/apidocs/org/apache/http/client/ResponseHandler.html ResponseHandler} which serves
  # as a Ruby proxy for HTTPClient responses.
  #
  # @!attribute [r] headers
  #   @return [Hash] Headers from this response. If a header is given more than once in a response, the value is an array of values. Otherwise, it is the header value.
  # @!attribute [r] code
  #   @return [Integer] Response code from this response
  # @!attribute [r] context
  #   @return [HttpContext] Context associated with this request/response
  # @!attribute [r] callback_result
  #   @return Value returned from any given on_success/response block
  class Response
    include_package "org.apache.http.client"
    include_package "org.apache.http.util"
    include_package "org.apache.http.protocol"
    java_import "org.apache.http.client.protocol.HttpClientContext"
    java_import "java.util.concurrent.Callable"

    include ResponseHandler
    include Callable

    attr_accessor :background
    attr_reader :context, :request, :callback_result, :called, :future

    # Creates a new Response
    #
    # @param  client             [Manticore::Client] The client that was used to create this response
    # @param  request            [HttpRequestBase] The underlying request object
    # @param  context            [HttpContext] The underlying HttpContext
    def initialize(client, request, context, &block)
      @client = client
      @request = request
      @context = context
      @handlers = {
        success: block || Proc.new { |resp| resp.body },
        failure: Proc.new { |ex| raise ex },
        cancelled: Proc.new { },
        complete: [],
      }
    end

    # Implementation of Callable#call
    # Used by Manticore::Client to invoke the request tied to this response.
    def call
      return background! if @background
      raise "Already called" if @called
      @called = true
      begin
        @client.client.execute @request, self, @context
      rescue Java::JavaNet::SocketTimeoutException => e
        ex = Manticore::SocketTimeout
      rescue Java::OrgApacheHttpConn::ConnectTimeoutException => e
        ex = Manticore::ConnectTimeout
      rescue Java::JavaNet::SocketException => e
        ex = Manticore::SocketException
      rescue Java::OrgApacheHttpClient::ClientProtocolException, Java::JavaxNetSsl::SSLHandshakeException, Java::OrgApacheHttpConn::HttpHostConnectException,
             Java::OrgApacheHttp::NoHttpResponseException, Java::OrgApacheHttp::ConnectionClosedException => e
        ex = Manticore::ClientProtocolException
      rescue Java::JavaNet::UnknownHostException => e
        ex = Manticore::ResolutionFailure
      rescue Java::JavaLang::IllegalArgumentException => e
        ex = Manticore::InvalidArgumentException
      rescue Java::JavaLang::IllegalStateException => e
        if e.message.match(/Connection pool shut down/)
          ex = Manticore::ClientStoppedException
        else
          @exception = e
        end
      rescue Java::JavaLang::Exception => e # Handle anything we may have missed from java
        ex = Manticore::UnknownException
      rescue StandardError => e
        @exception = e
      end

      # TODO: If calling async, execute_complete may fail and then silently swallow exceptions. How do we fix that?
      if ex || @exception
        @exception ||= ex.new(e.cause || e.message)
        @handlers[:failure].call @exception
        execute_complete
        nil
      else
        execute_complete
        self
      end
    end

    # Fetch the final resolved URL for this response. Will call the request if it has not been called yet.
    #
    # @return [String]
    def final_url
      call_once
      last_request = context.get_attribute ExecutionContext.HTTP_REQUEST
      last_host = context.get_attribute ExecutionContext.HTTP_TARGET_HOST
      host = last_host.to_uri
      url = last_request.get_uri
      URI.join(host, url.to_s)
    end

    # Fetch the body content of this response. Will call the request if it has not been called yet.
    # This fetches the input stream in Ruby; this isn't optimal, but it's faster than
    # fetching the whole thing in Java then UTF-8 encoding it all into a giant Ruby string.
    #
    # This permits for streaming response bodies, as well.
    #
    # @example Streaming response
    #
    #     client.get("http://example.com/resource").on_success do |response|
    #       response.body do |chunk|
    #         # Do something with chunk, which is a parsed portion of the returned body
    #       end
    #     end
    #
    # @return [String] Reponse body
    def body(&block)
      call_once
      @body ||= begin
                  if entity = @response.get_entity
                    EntityConverter.new.read_entity(entity, &block)
                  end
                rescue Java::JavaIo::IOException, Java::JavaNet::SocketException, IOError => e
                  raise StreamClosedException.new("Could not read from stream: #{e.message}")
                  # ensure
                  #   @request.release_connection
                end
    end

    alias_method :read_body, :body

    # Returns true if this response has been called (requested and populated) yet
    def called?
      !!@called
    end

    # Return a hash of headers from this response. Will call the request if it has not been called yet.
    #
    # @return [Array<string, obj>] Hash of headers. Keys will be lower-case.
    def headers
      call_once
      @headers
    end

    # Return the value of a single response header. Will call the request if it has not been called yet.
    # If the header was returned with multiple values, will only return the first value. If you need to get
    # multiple values, use response#headers[lowercase_key]
    #
    # @param  key [String] Case-insensitive header key
    # @return     [String] Value of the header, or nil if not present
    def [](key)
      v = headers[key.downcase]
      v.is_a?(Array) ? v.first : v
    end

    # Return the response code from this request as an integer. Will call the request if it has not been called yet.
    #
    # @return [Integer] The response code
    def code
      call_once
      @code
    end

    # Return the response text for a request as a string (Not Found, Ok, Bad Request, etc). Will call the request if it has not been called yet.
    #
    # @return [String] The response code text
    def message
      call_once
      @message
    end

    # Returns the length of the response body. Returns -1 if content-length is not present in the response.
    #
    # @return [Integer]
    def length
      (headers["content-length"] || -1).to_i
    end

    # Returns an array of {Manticore::Cookie Cookies} associated with this request's execution context
    #
    # @return [Array<Manticore::Cookie>]
    def cookies
      call_once
      @cookies ||= begin
        @context.get_cookie_store.get_cookies.inject({}) do |all, java_cookie|
          c = Cookie.from_java(java_cookie)
          all[c.name] ||= []
          all[c.name] << c
          all
        end
      end
    end

    # Set handler for success responses
    # @param block Proc which will be invoked on a successful response. Block will receive |response, request|
    #
    # @return self
    def on_success(&block)
      @handlers[:success] = block
      self
    end

    alias_method :success, :on_success

    # Set handler for failure responses
    # @param block Proc which will be invoked on a on a failed response. Block will receive an exception object.
    #
    # @return self
    def on_failure(&block)
      @handlers[:failure] = block
      self
    end

    alias_method :failure, :on_failure
    alias_method :fail, :on_failure

    # Set handler for cancelled requests. NB: Not actually used right now?
    # @param block Proc which will be invoked on a on a cancelled response.
    #
    # @return self
    def on_cancelled(&block)
      @handlers[:cancelled] = block
      self
    end

    alias_method :cancelled, :on_cancelled
    alias_method :cancellation, :on_cancelled
    alias_method :on_cancellation, :on_cancelled

    # Set handler for completed requests
    # @param block Proc which will be invoked on a on a completed response. This handler will be called
    #                   regardless of request success/failure.
    # @return self
    def on_complete(&block)
      @handlers[:complete] = Array(@handlers[:complete]).compact + [block]
      self
    end

    alias_method :complete, :on_complete
    alias_method :completed, :on_complete
    alias_method :on_completed, :on_complete

    def times_retried
      @context.get_attribute("retryCount") || 0
    end

    private

    def background!
      @background = false
      @future ||= @client.executor.java_method(:submit, [java.util.concurrent.Callable.java_class]).call(self)
    end

    # Implementation of {http://hc.apache.org/httpcomponents-client-ga/httpclient/apidocs/org/apache/http/client/ResponseHandler.html#handleResponse(org.apache.http.HttpResponse) ResponseHandler#handleResponse}
    # @param  response [Response] The underlying Java Response object
    def handleResponse(response)
      @response = response
      @code = response.get_status_line.get_status_code
      @message = response.get_status_line.get_reason_phrase
      @headers = response.get_all_headers.each_with_object({}) do |h, o|
        key = h.get_name.downcase
        if o.key?(key)
          o[key] = Array(o[key]) unless o[key].is_a?(Array)
          o[key].push h.get_value
        else
          o[key] = h.get_value
        end
      end

      @callback_result = @handlers[:success].call(self)
      nil
    end

    def call_once
      is_background = @background
      call unless called?
      # If this is a background request, then we don't want to allow the usage of sync methods, as it's probably a semantic error. We could resolve the future
      # but that'll probably result in blocking foreground threads unintentionally. Fail loudly.
      raise RuntimeError.new("Cannot call synchronous methods on a background response. Use an on_success handler instead.") if is_background && @future
      @called = true
    end

    def execute_complete
      @handlers[:complete].each { |h| h.call(self) }
    end
  end
end
