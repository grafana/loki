# encoding: utf-8
require "logstash/devutils/rspec/spec_helper"
require "logstash/outputs/loki"
require "logstash/codecs/plain"
require "logstash/event"
require "net/http"
include Loki

describe Loki::Entry do
  context 'test entry generation' do
      let (:event) {
        LogStash::Event.new(
            {
              'message' => 'hello',
              '@metadata' => {'foo'=>'bar'},
              '@version' => '1',
              'foo' => 5,
              'agent' => 'filebeat',
              'log' => {
                   'file' =>
                        {'@path' => '/path/to/file.log'},
                   },
              'host' => '172.0.0.1',
              'trace_id' => 'trace_001',
              '@timestamp' => Time.now
            }
        )
      }

      it 'labels extracted should not contains object and metadata or timestamp' do
        entry = Entry.new(event,"message", [], [])
        expect(entry.labels).to eql({ 'agent' => 'filebeat', 'host' => '172.0.0.1', 'foo'=>'5', 'trace_id'=>'trace_001'})
        expect(entry.entry['ts']).to eql to_ns(event.get("@timestamp"))
        expect(entry.entry['line']).to eql 'hello'
      end

      it 'labels extracted should only contain allowlisted labels' do
        entry = Entry.new(event, "message", %w[agent foo], [])
        expect(entry.labels).to eql({ 'agent' => 'filebeat', 'foo'=>'5'})
        expect(entry.entry['ts']).to eql to_ns(event.get("@timestamp"))
        expect(entry.entry['line']).to eql 'hello'
      end

      it 'labels and structured metadata extracted should only contain allow listed labels and metadata' do
        entry = Entry.new(event, "message", %w[agent foo], %w[trace_id])
        expect(entry.labels).to eql({ 'agent' => 'filebeat', 'foo'=>'5'})
        expect(entry.entry['ts']).to eql to_ns(event.get("@timestamp"))
        expect(entry.entry['line']).to eql 'hello'
        expect(entry.entry['metadata']).to eql({'trace_id' => 'trace_001'})
      end
  end

  context 'test batch generation with label order' do
    let (:entries)  {[
        Entry.new(LogStash::Event.new({"message"=>"foobuzz","buzz"=>"bar","cluster"=>"us-central1","@timestamp"=>Time.at(1)}),"message", [], []),
        Entry.new(LogStash::Event.new({"log"=>"foobar","bar"=>"bar","@timestamp"=>Time.at(2)}),"log", [], []),
        Entry.new(LogStash::Event.new({"cluster"=>"us-central1","message"=>"foobuzz","buzz"=>"bar","@timestamp"=>Time.at(3)}),"message", [], []),

    ]}
    let (:expected) {
      {"streams" => [
          {"stream"=> {"buzz"=>"bar","cluster"=>"us-central1"}, "values" => [[to_ns(Time.at(1)).to_s,"foobuzz"],[to_ns(Time.at(3)).to_s,"foobuzz"]]},
          {"stream"=> {"bar"=>"bar"}, "values"=>[[to_ns(Time.at(2)).to_s,"foobar"]]},
      ] }
    }

    it 'to_json' do
      @batch = Loki::Batch.new(entries.first)
      entries.drop(1).each { |e| @batch.add(e)}
      expect(JSON.parse(@batch.to_json)).to eql expected
    end
  end


end
