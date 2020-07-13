# frozen_string_literal: true

RSpec.describe Faraday::Adapter::HTTPClient do
  # ruby gem defaults for testing purposes
  HTTPCLIENT_OPEN = 60
  HTTPCLIENT_READ = 60
  HTTPCLIENT_WRITE = 120

  features :request_body_on_query_methods, :reason_phrase_parse, :compression,
           :trace_method, :local_socket_binding

  it_behaves_like 'an adapter'

  it 'allows to provide adapter specific configs' do
    adapter = described_class.new do |client|
      client.keep_alive_timeout = 20
      client.ssl_config.timeout = 25
    end

    client = adapter.build_connection(url: URI.parse('https://example.com'))
    expect(client.keep_alive_timeout).to eq(20)
    expect(client.ssl_config.timeout).to eq(25)
  end

  context 'Options' do
    let(:request) { Faraday::RequestOptions.new }
    let(:env) { { request: request } }
    let(:options) { {} }
    let(:adapter) { Faraday::Adapter::HTTPClient.new }
    let(:client) { adapter.connection(url: URI.parse('https://example.com')) }

    it 'configures timeout' do
      assert_default_timeouts!

      request.timeout = 5
      adapter.configure_timeouts(client, request)

      expect(client.connect_timeout).to eq(5)
      expect(client.send_timeout).to eq(5)
      expect(client.receive_timeout).to eq(5)
    end

    it 'configures open timeout' do
      assert_default_timeouts!

      request.open_timeout = 1
      adapter.configure_timeouts(client, request)

      expect(client.connect_timeout).to eq(1)
      expect(client.send_timeout).to eq(HTTPCLIENT_WRITE)
      expect(client.receive_timeout).to eq(HTTPCLIENT_READ)
    end

    it 'configures multiple timeouts' do
      assert_default_timeouts!

      request.open_timeout = 1
      request.write_timeout = 10
      request.read_timeout = 5
      adapter.configure_timeouts(client, request)

      expect(client.connect_timeout).to eq(1)
      expect(client.send_timeout).to eq(10)
      expect(client.receive_timeout).to eq(5)
    end

    def assert_default_timeouts!
      expect(client.connect_timeout).to eq(HTTPCLIENT_OPEN)
      expect(client.send_timeout).to eq(HTTPCLIENT_WRITE)
      expect(client.receive_timeout).to eq(HTTPCLIENT_READ)
    end
  end
end
