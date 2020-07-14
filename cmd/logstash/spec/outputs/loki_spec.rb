# encoding: utf-8
require "logstash/devutils/rspec/spec_helper"
require "logstash/outputs/loki"
require "logstash/codecs/plain"
require "logstash/event"
require "net/http"

describe LogStash::Outputs::Loki do

  let (:simple_loki_config) {
    {'url' => 'http://localhost:3100'}
  }

  context 'when initializing' do
    it "should register" do
      loki = LogStash::Plugin.lookup("output", "loki").new(simple_loki_config)
      expect { loki.register }.to_not raise_error
    end

    it 'should populate loki config with default or initialized values' do
      loki = LogStash::Outputs::Loki.new(simple_loki_config)
      expect(loki.url).to eql 'http://localhost:3100'
      expect(loki.tenant_id).to eql nil
      expect(loki.batch_size).to eql 102400
      expect(loki.batch_wait).to eql 1
    end
  end


  context 'test http requests' do
    let (:simple_loki_config) {{'url' => 'http://localhost:3100', 'include_labels' => ["@version", "host", "test"],}}
    let (:event) { LogStash::Event.new({'message' => 'hello', '@version' => '1', 'host' => '172.0.0.1',
                                      '@timestamp' => LogStash::Timestamp.now}) }
    let(:loki) { LogStash::Plugin.lookup("output", "loki").new(simple_loki_config) }

    before do
      loki.register
      loki.close
    end

    it 'test http requests when' do
      labels = {}
      event_hash = event.to_hash
      lbls = loki.handle_labels(event_hash, labels, "")
      entry_hash = {
        "ts" => event.get("@timestamp").to_i * (10**9),
        "line" => event.get("message").to_s
      }
      e = LogStash::Outputs::Loki::Entry.new(lbls, entry_hash)
      batch = LogStash::Outputs::Loki::Batch.new(e)
      payload = loki.build_payload(batch)

      # response should be nil on connection error
      expect(loki.loki_http_request("fake", payload, 1, 2, 3)).to eql nil

      success = Net::HTTPSuccess.new(1.0, 200, 'OK')
      allow(loki).to receive(:loki_http_request) { success }
      allow(success).to receive(:payload).and_return('fake body')
      expect(loki.loki_http_request("fake", batch, 1, 300, 10).class).to eql Net::HTTPSuccess

      too_many_requests = Net::HTTPTooManyRequests.new(1.0, 429, 'OK')
      allow(loki).to receive(:loki_http_request) { too_many_requests }
      allow(too_many_requests).to receive(:payload).and_return('fake body')
      expect(loki.loki_http_request("fake", batch, 1, 300, 10).class).to eql Net::HTTPTooManyRequests

      server_error = Net::HTTPServerError.new(1.0, 429, 'OK')
      allow(loki).to receive(:loki_http_request) { server_error }
      allow(server_error).to receive(:payload).and_return('fake body')
      expect(loki.loki_http_request("fake", batch, 1, 300, 10).class).to eql Net::HTTPServerError
    end
  end
end
