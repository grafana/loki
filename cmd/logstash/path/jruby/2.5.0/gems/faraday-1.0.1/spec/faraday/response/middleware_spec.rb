# frozen_string_literal: true

RSpec.describe Faraday::Response::Middleware do
  let(:conn) do
    Faraday.new do |b|
      b.use custom_middleware
      b.adapter :test do |stub|
        stub.get('ok') { [200, { 'Content-Type' => 'text/html' }, '<body></body>'] }
        stub.get('not_modified') { [304, nil, nil] }
        stub.get('no_content') { [204, nil, nil] }
      end
    end
  end

  context 'with a custom ResponseMiddleware' do
    let(:custom_middleware) do
      Class.new(Faraday::Response::Middleware) do
        def parse(body)
          body.upcase
        end
      end
    end

    it 'parses the response' do
      expect(conn.get('ok').body).to eq('<BODY></BODY>')
    end
  end

  context 'with a custom ResponseMiddleware and private parse' do
    let(:custom_middleware) do
      Class.new(Faraday::Response::Middleware) do
        private

        def parse(body)
          body.upcase
        end
      end
    end

    it 'parses the response' do
      expect(conn.get('ok').body).to eq('<BODY></BODY>')
    end
  end

  context 'with a custom ResponseMiddleware but empty response' do
    let(:custom_middleware) do
      Class.new(Faraday::Response::Middleware) do
        def parse(_body)
          raise 'this should not be called'
        end
      end
    end

    it 'raises exception for 200 responses' do
      expect { conn.get('ok') }.to raise_error(StandardError)
    end

    it 'doesn\'t call the middleware for 204 responses' do
      expect_any_instance_of(custom_middleware).not_to receive(:parse)
      expect(conn.get('no_content').body).to be_nil
    end

    it 'doesn\'t call the middleware for 304 responses' do
      expect_any_instance_of(custom_middleware).not_to receive(:parse)
      expect(conn.get('not_modified').body).to be_nil
    end
  end
end
