module Manticore
  # StubbedResponse is a special subclass of Response that may be used to test your code that uses Manticore
  # without actually having Manticore make requests.
  #
  # @example Standard usage with Rspec
  #
  #   response = Manticore::StubbedResponse.stub(body: "body", code: 200).call
  #   Manticore.should_receive(:get).with("http://www.google.com/").and_return(response)
  #   response.body.should == "body"
  #   response.code.should == 200
  #
  # @since 0.3.0
  class StubbedResponse < Response

    # Helper that instantiates a Response and stubs out its body, response code, etc
    # @param  stubs [Hash] Hash of parameters to stub. See #stub.
    # @return [Manticore::StubbedResponse]
    def self.stub(stubs = {})
      new.stub(stubs)
    end

    def initialize(client = nil, request = nil, context = nil)
      super
    end

    # Stub out a Manticore::RequestStub.
    #
    # @param  stubs [Hash] Parameters to stub out
    # @option stubs [String]                   body  The body that should be returned
    # @option stubs [Integer]                  code  Response code to simulate
    # @option stubs [Hash]                     headers  Response headers as a hash
    # @option stubs [Array<Manticore::Cookie>] cookies
    # @return [Manticore::StubbedResponse] self
    def stub(stubs)
      if stubs.key? :cookies
        stubs[:cookies].keys.each { |key| stubs[:cookies][key] = Array(stubs[:cookies][key]) }
      end
      stubs[:code] ||= 200

      stubs[:headers] ||= {}
      stubs[:headers] = Hash[*stubs[:headers].flat_map { |k, v| [k.downcase, v] }]
      stubs[:headers]["content-length"] ||= stubs[:body].length.to_s if stubs.key?(:body)

      @stubs = stubs

      self
    end

    # Used by Manticore::Client to invoke the request tied to this response
    def call
      @called = true
      handleResponse @stubs
    end

    # Simulates the final URL of a redirected request. Returns the value of headers["location"]
    #
    # @return [String] The final URL
    def final_url
      call_once
      @headers["location"] || @request.getURI.to_string
    end

    # Returns the stubbed body of this response.
    def body(&block)
      call_once
      if block_given?
        yield body
      else
        @body
      end
    end

    alias_method :read_body, :body

    # Returns the stubbed cookies of this response. This is the union of cookies from the `:cookies`
    # option key and any `set-cookie` headers passed.
    def cookies
      call_once
      @cookies
    end

    private

    def handleResponse(response)
      raise response[:raises] if response.key?(:raises)
      @body = response[:body]
      @code = response[:code]
      @headers = response[:headers]
      @cookies = response[:cookies]
      Array(@headers["set-cookie"]).each do |cookie|
        c = Cookie.from_set_cookie(cookie)
        @cookies[c.name] ||= []
        @cookies[c.name] << c
      end
      @callback_result = @handlers[:success].call(self)
      self
    end

    def call_once
      call unless @called
    end
  end
end
