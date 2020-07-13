# frozen_string_literal: true

RSpec.describe Faraday::Adapter::Excon do
  features :request_body_on_query_methods, :reason_phrase_parse, :trace_method

  it_behaves_like 'an adapter'

  it 'allows to provide adapter specific configs' do
    url = URI('https://example.com:1234')

    adapter = described_class.new(nil, debug_request: true)

    conn = adapter.build_connection(url: url)

    expect(conn.data[:debug_request]).to be_truthy
  end

  context 'config' do
    let(:adapter) { Faraday::Adapter::Excon.new }
    let(:request) { Faraday::RequestOptions.new }
    let(:uri) { URI.parse('https://example.com') }
    let(:env) { { request: request, url: uri } }

    it 'sets timeout' do
      request.timeout = 5
      options = adapter.send(:opts_from_env, env)
      expect(options[:read_timeout]).to eq(5)
      expect(options[:write_timeout]).to eq(5)
      expect(options[:connect_timeout]).to eq(5)
    end

    it 'sets timeout and open_timeout' do
      request.timeout = 5
      request.open_timeout = 3
      options = adapter.send(:opts_from_env, env)
      expect(options[:read_timeout]).to eq(5)
      expect(options[:write_timeout]).to eq(5)
      expect(options[:connect_timeout]).to eq(3)
    end

    it 'sets open_timeout' do
      request.open_timeout = 3
      options = adapter.send(:opts_from_env, env)
      expect(options[:read_timeout]).to eq(nil)
      expect(options[:write_timeout]).to eq(nil)
      expect(options[:connect_timeout]).to eq(3)
    end
  end
end
