# encoding: utf-8
require "logstash/devutils/rspec/spec_helper"
require "logstash/outputs/loki"
require "logstash/codecs/plain"
require "logstash/event"
require "net/http"
require 'webmock/rspec'
include Loki

describe LogStash::Outputs::Loki do

  let (:simple_loki_config) { {'url' => 'http://localhost:3100'} }

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

  context 'when adding en entry to the batch' do
    let (:simple_loki_config) {{'url' => 'http://localhost:3100'}}
    let (:entry) {Entry.new(LogStash::Event.new({"message"=>"foobuzz","buzz"=>"bar","cluster"=>"us-central1","@timestamp"=>Time.at(1)}),"message", [])}
    let (:lbs) {{"buzz"=>"bar","cluster"=>"us-central1"}.sort.to_h}
    let (:include_loki_config) {{ 'url' => 'http://localhost:3100', 'include_fields' => ["cluster"] }}
    let (:include_entry) {Entry.new(LogStash::Event.new({"message"=>"foobuzz","buzz"=>"bar","cluster"=>"us-central1","@timestamp"=>Time.at(1)}),"message", ["cluster"])}
    let (:include_lbs) {{"cluster"=>"us-central1"}.sort.to_h}

    it 'should not add empty line' do
      plugin = LogStash::Plugin.lookup("output", "loki").new(simple_loki_config)
      emptyEntry = Entry.new(LogStash::Event.new({"message"=>"foobuzz","buzz"=>"bar","cluster"=>"us-central1","@timestamp"=>Time.at(1)}),"foo", [])
      expect(plugin.add_entry_to_batch(emptyEntry)).to eql true
      expect(plugin.batch).to eql nil
    end

    it 'should add entry' do
      plugin = LogStash::Plugin.lookup("output", "loki").new(simple_loki_config)
      expect(plugin.batch).to eql nil
      expect(plugin.add_entry_to_batch(entry)).to eql true
      expect(plugin.add_entry_to_batch(entry)).to eql true
      expect(plugin.batch).not_to be_nil
      expect(plugin.batch.streams.length).to eq 1
      expect(plugin.batch.streams[lbs.to_s]['entries'].length).to eq 2
      expect(plugin.batch.streams[lbs.to_s]['labels']).to eq lbs
      expect(plugin.batch.size_bytes).to eq 14
    end

    it 'should only allowed labels defined in include_fields' do
      plugin = LogStash::Plugin.lookup("output", "loki").new(include_loki_config)
      expect(plugin.batch).to eql nil
      expect(plugin.add_entry_to_batch(include_entry)).to eql true
      expect(plugin.add_entry_to_batch(include_entry)).to eql true
      expect(plugin.batch).not_to be_nil
      expect(plugin.batch.streams.length).to eq 1
      expect(plugin.batch.streams[include_lbs.to_s]['entries'].length).to eq 2
      expect(plugin.batch.streams[include_lbs.to_s]['labels']).to eq include_lbs
      expect(plugin.batch.size_bytes).to eq 14
    end

    it 'should not add if full' do
      plugin = LogStash::Plugin.lookup("output", "loki").new(simple_loki_config.merge!({'batch_size'=>10}))
      expect(plugin.batch).to eql nil
      expect(plugin.add_entry_to_batch(entry)).to eql true # first entry is fine.
      expect(plugin.batch).not_to be_nil
      expect(plugin.batch.streams.length).to eq 1
      expect(plugin.batch.streams[lbs.to_s]['entries'].length).to eq 1
      expect(plugin.batch.streams[lbs.to_s]['labels']).to eq lbs
      expect(plugin.batch.size_bytes).to eq 7
      expect(plugin.add_entry_to_batch(entry)).to eql false # second entry goes over the limit.
      expect(plugin.batch).not_to be_nil
      expect(plugin.batch.streams.length).to eq 1
      expect(plugin.batch.streams[lbs.to_s]['entries'].length).to eq 1
      expect(plugin.batch.streams[lbs.to_s]['labels']).to eq lbs
      expect(plugin.batch.size_bytes).to eq 7
    end
  end

  context 'batch expiration' do
    let (:entry) {Entry.new(LogStash::Event.new({"message"=>"foobuzz","buzz"=>"bar","cluster"=>"us-central1","@timestamp"=>Time.at(1)}),"message", [])}

    it 'should not expire if empty' do
      loki = LogStash::Outputs::Loki.new(simple_loki_config.merge!({'batch_wait'=>0.5}))
      sleep(1)
      expect(loki.is_batch_expired).to be false
    end
    it 'should not expire batch if not old' do
      loki = LogStash::Outputs::Loki.new(simple_loki_config.merge!({'batch_wait'=>0.5}))
      expect(loki.add_entry_to_batch(entry)).to eql true
      expect(loki.is_batch_expired).to be false
    end
    it 'should expire if old' do
      loki = LogStash::Outputs::Loki.new(simple_loki_config.merge!({'batch_wait'=>0.5}))
      expect(loki.add_entry_to_batch(entry)).to eql true
      sleep(1)
      expect(loki.is_batch_expired).to be true
    end
  end

  context 'channel' do
    let (:event) {LogStash::Event.new({"message"=>"foobuzz","buzz"=>"bar","cluster"=>"us-central1","@timestamp"=>Time.at(1)})}

    it 'should send entry if batch size reached with no tenant' do
      loki = LogStash::Outputs::Loki.new(simple_loki_config.merge!({'batch_wait'=>0.5,'batch_size'=>10}))
      loki.register
      sent = Queue.new
      allow(loki).to receive(:send) do |batch|
        Thread.new do
          sent << batch
        end
      end
      loki.receive(event)
      loki.receive(event)
      sent.deq
      sent.deq
      loki.close
    end
    it 'should send entry while closing' do
      loki = LogStash::Outputs::Loki.new(simple_loki_config.merge!({'batch_wait'=>10,'batch_size'=>10}))
      loki.register
      sent = Queue.new
      allow(loki).to receive(:send) do | batch|
        Thread.new  do
          sent << batch
        end
      end
      loki.receive(event)
      loki.close
      sent.deq
    end
    it 'should send entry when batch is expiring' do
      loki = LogStash::Outputs::Loki.new(simple_loki_config.merge!({'batch_wait'=>0.5,'batch_size'=>10}))
      loki.register
      sent = Queue.new
      allow(loki).to receive(:send) do | batch|
        Thread.new  do
          sent << batch
        end
      end
      loki.receive(event)
      sent.deq
      sleep(0.01) # Adding a minimal sleep. In few cases @batch=nil might happen after evaluating for nil
      expect(loki.batch).to be_nil 
      loki.close
    end
  end

  context 'http requests' do
    let (:entry) {Entry.new(LogStash::Event.new({"message"=>"foobuzz","buzz"=>"bar","cluster"=>"us-central1","@timestamp"=>Time.at(1)}),"message", [])}

    it 'should send credentials' do
      conf = {
          'url'=>'http://localhost:3100/loki/api/v1/push',
          'username' => 'foo',
          'password' => 'bar',
          'tenant_id' => 't'
      }
      loki = LogStash::Outputs::Loki.new(conf)
      loki.register
      b = Batch.new(entry)
      post = stub_request(:post, "http://localhost:3100/loki/api/v1/push").with(
          basic_auth: ['foo', 'bar'],
          body: b.to_json,
          headers:{
              'Content-Type' => 'application/json' ,
              'User-Agent' => 'loki-logstash',
              'X-Scope-OrgID'=>'t',
              'Accept'=>'*/*',
              'Accept-Encoding'=>'gzip;q=1.0,deflate;q=0.6,identity;q=0.3',
          }
      )
      loki.send(b)
      expect(post).to have_been_requested.times(1)
    end

    it 'should not send credentials' do
      conf = {
          'url'=>'http://foo.com/loki/api/v1/push',
      }
      loki = LogStash::Outputs::Loki.new(conf)
      loki.register
      b = Batch.new(entry)
      post = stub_request(:post, "http://foo.com/loki/api/v1/push").with(
          body: b.to_json,
          headers:{
              'Content-Type' => 'application/json' ,
              'User-Agent' => 'loki-logstash',
              'Accept'=>'*/*',
              'Accept-Encoding'=>'gzip;q=1.0,deflate;q=0.6,identity;q=0.3',
          }
      )
      loki.send(b)
      expect(post).to have_been_requested.times(1)
    end
    it 'should retry 500' do
      conf = {
          'url'=>'http://foo.com/loki/api/v1/push',
          'retries' => 3,
      }
      loki = LogStash::Outputs::Loki.new(conf)
      loki.register
      b = Batch.new(entry)
      post = stub_request(:post, "http://foo.com/loki/api/v1/push").with(
          body: b.to_json,
      ).to_return(status: [500, "Internal Server Error"])
      loki.send(b)
      loki.close
      expect(post).to have_been_requested.times(3)
    end
    it 'should retry 429' do
      conf = {
          'url'=>'http://foo.com/loki/api/v1/push',
          'retries' => 2,
      }
      loki = LogStash::Outputs::Loki.new(conf)
      loki.register
      b = Batch.new(entry)
      post = stub_request(:post, "http://foo.com/loki/api/v1/push").with(
          body: b.to_json,
          ).to_return(status: [429, "stop spamming"])
      loki.send(b)
      loki.close
      expect(post).to have_been_requested.times(2)
    end
    it 'should not retry 400' do
      conf = {
          'url'=>'http://foo.com/loki/api/v1/push',
          'retries' => 11,
      }
      loki = LogStash::Outputs::Loki.new(conf)
      loki.register
      b = Batch.new(entry)
      post = stub_request(:post, "http://foo.com/loki/api/v1/push").with(
          body: b.to_json,
          ).to_return(status: [400, "bad request"])
      loki.send(b)
      loki.close
      expect(post).to have_been_requested.times(1)
    end
    it 'should retry exception' do
      conf = {
          'url'=>'http://foo.com/loki/api/v1/push',
          'retries' => 11,
      }
      loki = LogStash::Outputs::Loki.new(conf)
      loki.register
      b = Batch.new(entry)
      post = stub_request(:post, "http://foo.com/loki/api/v1/push").with(
          body: b.to_json,
          ).to_raise("some error").then.to_return(status: [200, "fine !"])
      loki.send(b)
      loki.close
      expect(post).to have_been_requested.times(2)
    end
  end
end
