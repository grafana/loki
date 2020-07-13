# frozen_string_literal: true

# Rack app used to test the Rack adapter.
# Uses Webmock to check if requests are registered, in which case it returns
# the registered response.
class WebmockRackApp
  def call(env)
    req_signature = WebMock::RequestSignature.new(
      req_method(env),
      req_uri(env),
      body: req_body(env),
      headers: req_headers(env)
    )

    WebMock::RequestRegistry
      .instance
      .requested_signatures
      .put(req_signature)

    process_response(req_signature)
  end

  def req_method(env)
    env['REQUEST_METHOD'].downcase.to_sym
  end

  def req_uri(env)
    scheme = env['rack.url_scheme']
    host = env['SERVER_NAME']
    port = env['SERVER_PORT']
    path = env['PATH_INFO']
    query = env['QUERY_STRING']

    url = +"#{scheme}://#{host}:#{port}#{path}"
    url += "?#{query}" if query

    uri = WebMock::Util::URI.heuristic_parse(url)
    uri.path = uri.normalized_path.gsub('[^:]//', '/')
    uri
  end

  def req_headers(env)
    http_headers = env.select { |k, _| k.start_with?('HTTP_') }
                      .map { |k, v| [k[5..-1], v] }
                      .to_h

    special_headers = Faraday::Adapter::Rack::SPECIAL_HEADERS
    http_headers.merge(env.select { |k, _| special_headers.include?(k) })
  end

  def req_body(env)
    env['rack.input'].read
  end

  def process_response(req_signature)
    res = WebMock::StubRegistry.instance.response_for_request(req_signature)

    if res.nil? && req_signature.uri.host == 'localhost'
      raise Faraday::ConnectionFailed, 'Trying to connect to localhost'
    end

    raise WebMock::NetConnectNotAllowedError, req_signature unless res

    raise Faraday::TimeoutError if res.should_timeout

    [res.status[0], res.headers || {}, [res.body || '']]
  end
end
