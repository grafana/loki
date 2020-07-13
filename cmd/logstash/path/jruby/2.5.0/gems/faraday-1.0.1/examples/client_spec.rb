# frozen_string_literal: true

# Requires Ruby with rspec and faraday gems.
# rspec client_spec.rb

require 'faraday'
require 'json'

# Example API client
class Client
  def initialize(conn)
    @conn = conn
  end

  def sushi(jname)
    res = @conn.get("/#{jname}")
    data = JSON.parse(res.body)
    data['name']
  end
end

Rspec.describe Client do
  let(:stubs)  { Faraday::Adapter::Test::Stubs.new }
  let(:conn)   { Faraday.new { |b| b.adapter(:test, stubs) } }
  let(:client) { Client.new(conn) }

  it 'parses name' do
    stubs.get('/ebi') do |env|
      # optional: you can inspect the Faraday::Env
      expect(env.url.path).to eq('/ebi')
      [
        200,
        { 'Content-Type': 'application/javascript' },
        '{"name": "shrimp"}'
      ]
    end

    # uncomment to trigger stubs.verify_stubbed_calls failure
    # stubs.get('/unused') { [404, {}, ''] }

    expect(client.sushi('ebi')).to eq('shrimp')
    stubs.verify_stubbed_calls
  end

  it 'handles 404' do
    stubs.get('/ebi') do
      [
        404,
        { 'Content-Type': 'application/javascript' },
        '{}'
      ]
    end
    expect(client.sushi('ebi')).to be_nil
    stubs.verify_stubbed_calls
  end

  it 'handles exception' do
    stubs.get('/ebi') do
      raise Faraday::ConnectionFailed, nil
    end

    expect { client.sushi('ebi') }.to raise_error(Faraday::ConnectionFailed)
    stubs.verify_stubbed_calls
  end
end
