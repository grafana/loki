# frozen_string_literal: true

RSpec.describe Faraday::Adapter::NetHttp do
  features :request_body_on_query_methods, :reason_phrase_parse, :compression, :streaming, :trace_method

  it_behaves_like 'an adapter'

  context 'checking http' do
    let(:url) { URI('http://example.com') }
    let(:adapter) { described_class.new }
    let(:http) { adapter.send(:connection, url: url, request: {}) }

    it { expect(http.port).to eq(80) }

    it 'sets max_retries to 0' do
      adapter.send(:configure_request, http, {})

      expect(http.max_retries).to eq(0) if http.respond_to?(:max_retries=)
    end

    it 'supports write_timeout' do
      adapter.send(:configure_request, http, write_timeout: 10)

      expect(http.write_timeout).to eq(10) if http.respond_to?(:write_timeout=)
    end

    it 'supports open_timeout' do
      adapter.send(:configure_request, http, open_timeout: 10)

      expect(http.open_timeout).to eq(10)
    end

    it 'supports read_timeout' do
      adapter.send(:configure_request, http, read_timeout: 10)

      expect(http.read_timeout).to eq(10)
    end

    context 'with https url' do
      let(:url) { URI('https://example.com') }

      it { expect(http.port).to eq(443) }
    end

    context 'with http url including port' do
      let(:url) { URI('https://example.com:1234') }

      it { expect(http.port).to eq(1234) }
    end

    context 'with custom adapter config' do
      let(:adapter) do
        described_class.new do |http|
          http.continue_timeout = 123
        end
      end

      it do
        adapter.send(:configure_request, http, {})
        expect(http.continue_timeout).to eq(123)
      end
    end
  end
end
