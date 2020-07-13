# frozen_string_literal: true

RSpec.describe Faraday::Request::Authorization do
  let(:conn) do
    Faraday.new do |b|
      b.request auth_type, *auth_config
      b.adapter :test do |stub|
        stub.get('/auth-echo') do |env|
          [200, {}, env[:request_headers]['Authorization']]
        end
      end
    end
  end

  shared_examples 'does not interfere with existing authentication' do
    context 'and request already has an authentication header' do
      let(:response) { conn.get('/auth-echo', nil, authorization: 'Token token="bar"') }

      it 'does not interfere with existing authorization' do
        expect(response.body).to eq('Token token="bar"')
      end
    end
  end

  let(:response) { conn.get('/auth-echo') }

  describe 'basic_auth' do
    let(:auth_type) { :basic_auth }

    context 'when passed correct params' do
      let(:auth_config) { %w[aladdin opensesame] }

      it { expect(response.body).to eq('Basic YWxhZGRpbjpvcGVuc2VzYW1l') }

      include_examples 'does not interfere with existing authentication'
    end

    context 'when passed very long values' do
      let(:auth_config) { ['A' * 255, ''] }

      it { expect(response.body).to eq("Basic #{'QUFB' * 85}Og==") }

      include_examples 'does not interfere with existing authentication'
    end
  end

  describe 'token_auth' do
    let(:auth_type) { :token_auth }

    context 'when passed correct params' do
      let(:auth_config) { 'quux' }

      it { expect(response.body).to eq('Token token="quux"') }

      include_examples 'does not interfere with existing authentication'
    end

    context 'when other values are provided' do
      let(:auth_config) { ['baz', foo: 42] }

      it { expect(response.body).to match(/^Token /) }
      it { expect(response.body).to match(/token="baz"/) }
      it { expect(response.body).to match(/foo="42"/) }

      include_examples 'does not interfere with existing authentication'
    end
  end

  describe 'authorization' do
    let(:auth_type) { :authorization }

    context 'when passed two strings' do
      let(:auth_config) { ['custom', 'abc def'] }

      it { expect(response.body).to eq('custom abc def') }

      include_examples 'does not interfere with existing authentication'
    end

    context 'when passed a string and a hash' do
      let(:auth_config) { ['baz', foo: 42] }

      it { expect(response.body).to eq('baz foo="42"') }

      include_examples 'does not interfere with existing authentication'
    end
  end
end
