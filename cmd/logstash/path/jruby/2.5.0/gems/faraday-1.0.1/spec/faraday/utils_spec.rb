# frozen_string_literal: true

RSpec.describe Faraday::Utils do
  describe 'headers parsing' do
    let(:multi_response_headers) do
      "HTTP/1.x 500 OK\r\nContent-Type: text/html; charset=UTF-8\r\n" \
      "HTTP/1.x 200 OK\r\nContent-Type: application/json; charset=UTF-8\r\n\r\n"
    end

    it 'parse headers for aggregated responses' do
      headers = Faraday::Utils::Headers.new
      headers.parse(multi_response_headers)

      result = headers.to_hash

      expect(result['Content-Type']).to eq('application/json; charset=UTF-8')
    end
  end

  describe 'URI parsing' do
    let(:url) { 'http://example.com/abc' }

    it 'escapes safe buffer' do
      str = FakeSafeBuffer.new('$32,000.00')
      expect(Faraday::Utils.escape(str)).to eq('%2432%2C000.00')
    end

    it 'parses with default parser' do
      with_default_uri_parser(nil) do
        uri = normalize(url)
        expect(uri.host).to eq('example.com')
      end
    end

    it 'parses with URI' do
      with_default_uri_parser(::URI) do
        uri = normalize(url)
        expect(uri.host).to eq('example.com')
      end
    end

    it 'parses with block' do
      with_default_uri_parser(->(u) { "booya#{'!' * u.size}" }) do
        expect(normalize(url)).to eq('booya!!!!!!!!!!!!!!!!!!!!!!')
      end
    end

    it 'replaces headers hash' do
      headers = Faraday::Utils::Headers.new('authorization' => 't0ps3cr3t!')
      expect(headers).to have_key('authorization')

      headers.replace('content-type' => 'text/plain')
      expect(headers).not_to have_key('authorization')
    end
  end
end
