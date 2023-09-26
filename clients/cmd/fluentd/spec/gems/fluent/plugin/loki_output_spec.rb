# frozen_string_literal: true

require 'spec_helper'
require 'time'
require 'yajl'
require 'fluent/test'
require 'fluent/test/driver/output'
require 'fluent/test/helpers'

# prevent Test::Unit's AutoRunner from executing during RSpec's rake task
Test::Unit::AutoRunner.need_auto_run = false if defined?(Test::Unit::AutoRunner)

RSpec.describe Fluent::Plugin::LokiOutput do
  it 'loads config' do
    driver = Fluent::Test::Driver::Output.new(described_class)

    driver.configure(<<-CONF)
      type loki
      url https://logs-us-west1.grafana.net
      username userID
      password API_KEY
      tenant 1234
      extra_labels {}
      line_format key_value
      drop_single_key true
      remove_keys a, b
      insecure_tls true
      ciphers abc:def
      min_version TLS1_3
      <label>
        job
        instance instance
      </label>
    CONF

    expect(driver.instance.url).to eq 'https://logs-us-west1.grafana.net'
    expect(driver.instance.username).to eq 'userID'
    expect(driver.instance.password).to eq 'API_KEY'
    expect(driver.instance.tenant).to eq '1234'
    expect(driver.instance.extra_labels).to eq({})
    expect(driver.instance.line_format).to eq :key_value
    expect(driver.instance.record_accessors.keys).to eq %w[job instance]
    expect(driver.instance.remove_keys).to eq %w[a b]
    expect(driver.instance.drop_single_key).to eq true
    expect(driver.instance.insecure_tls).to eq true
    expect(driver.instance.ciphers).to eq 'abc:def'
    expect(driver.instance.min_version).to eq 'TLS1_3'
  end

  it 'converts syslog output to loki output' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    chunk = [Time.at(1_546_270_458), content[0]]
    payload = driver.instance.generic_to_loki([chunk])
    expect(payload[0]['stream'].empty?).to eq true
    expect(payload[0]['values'].count).to eq 1
    expect(payload[0]['values'][0][0]).to eq '1546270458000000000'
    expect(payload[0]['values'][0][1]).to eq content[0]
  end

  it 'converts syslog output with extra labels to loki output' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      extra_labels {"env": "test"}
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    chunk = [Time.at(1_546_270_458), content[0]]
    payload = driver.instance.generic_to_loki([chunk])
    expect(payload[0]['stream']).to eq('env' => 'test')
    expect(payload[0]['values'].count).to eq 1
    expect(payload[0]['values'][0][0]).to eq '1546270458000000000'
    expect(payload[0]['values'][0][1]).to eq content[0]
  end

  it 'converts multiple syslog output lines to loki output' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    line1 = [Time.at(1_546_270_458), content[0]]
    line2 = [Time.at(1_546_270_460), content[1]]
    payload = driver.instance.generic_to_loki([line1, line2])
    expect(payload[0]['stream'].empty?).to eq true
    expect(payload[0]['values'].count).to eq 2
    expect(payload[0]['values'][0][0]).to eq '1546270458000000000'
    expect(payload[0]['values'][0][1]).to eq content[0]
    expect(payload[0]['values'][1][0]).to eq '1546270460000000000'
    expect(payload[0]['values'][1][1]).to eq content[1]
  end

  it 'converts multiple syslog output lines with extra labels to loki output' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      extra_labels {"env": "test"}
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    line1 = [Time.at(1_546_270_458), content[0]]
    line2 = [Time.at(1_546_270_460), content[1]]
    payload = driver.instance.generic_to_loki([line1, line2])
    expect(payload[0]['stream']).to eq('env' => 'test')
    expect(payload[0]['values'].count).to eq 2
    expect(payload[0]['values'][0][0]).to eq '1546270458000000000'
    expect(payload[0]['values'][0][1]).to eq content[0]
    expect(payload[0]['values'][1][0]).to eq '1546270460000000000'
    expect(payload[0]['values'][1][1]).to eq content[1]
  end

  it 'removed non utf-8 characters from log lines' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/non_utf8.log')[0]
    chunk = [Time.at(1_546_270_458), { 'message' => content, 'number': 1.2345, 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([chunk])
    expect(payload[0]['stream'].empty?).to eq true
    expect(payload[0]['values'].count).to eq 1
    expect(payload[0]['values'][0][0]).to eq '1546270458000000000'
    expect(payload[0]['values'][0][1]).to eq 'message="? rest of line" number=1.2345 stream=stdout'
  end

  it 'handle non utf-8 characters from log lines in json format' do
    config = <<-CONF
      url         https://logs-us-west1.grafana.net
      line_format json
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/non_utf8.log')[0]
    chunk = [Time.at(1_546_270_458), { 'message' => content, 'number': 1.2345, 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([chunk])
    expect(payload[0]['stream'].empty?).to eq true
    expect(payload[0]['values'].count).to eq 1
    expect(payload[0]['values'][0][0]).to eq '1546270458000000000'
    expect(payload[0]['values'][0][1]).to eq(
      "{\"message\":\"\xC1 rest of line\",\"number\":1.2345,\"stream\":\"stdout\"}"
    )
  end

  it 'formats record hash as key_value' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog')
    line1 = [Time.at(1_546_270_458), { 'message' => content[0], 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['stream'].empty?).to eq true
    expect(body[:streams][0]['values'].count).to eq 1
    expect(body[:streams][0]['values'][0][0]).to eq '1546270458000000000'
    expect(body[:streams][0]['values'][0][1]).to eq "message=\"#{content[0]}\" stream=stdout"
  end

  it 'formats record hash as json' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      line_format json
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog')
    line1 = [Time.at(1_546_270_458), { 'message' => content[0], 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['stream'].empty?).to eq true
    expect(body[:streams][0]['values'].count).to eq 1
    expect(body[:streams][0]['values'][0][0]).to eq '1546270458000000000'
    expect(body[:streams][0]['values'][0][1]).to eq Yajl.dump(line1[1])
  end

  it 'extracts record key as label' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      line_format json
      <label>
        stream
      </label>
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog')
    line1 = [Time.at(1_546_270_458), { 'message' => content[0], 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['stream']).to eq('stream' => 'stdout')
    expect(body[:streams][0]['values'].count).to eq 1
    expect(body[:streams][0]['values'][0][0]).to eq '1546270458000000000'
    expect(body[:streams][0]['values'][0][1]).to eq Yajl.dump('message' => content[0])
  end

  it 'extracts nested record key as label' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      line_format json
      <label>
        pod $.kubernetes.pod
      </label>
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog')
    line1 = [Time.at(1_546_270_458), { 'message' => content[0], 'kubernetes' => { 'pod' => 'podname' } }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['stream']).to eq('pod' => 'podname')
    expect(body[:streams][0]['values'].count).to eq 1
    expect(body[:streams][0]['values'][0][0]).to eq '1546270458000000000'
    expect(body[:streams][0]['values'][0][1]).to eq Yajl.dump('message' => content[0], 'kubernetes' => {})
  end

  it 'extracts nested record key as label and drop key after' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      line_format json
      remove_keys kubernetes
      <label>
        pod $.kubernetes.pod
      </label>
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog')
    line1 = [Time.at(1_546_270_458), { 'message' => content[0], 'kubernetes' => { 'pod' => 'podname' } }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['stream']).to eq('pod' => 'podname')
    expect(body[:streams][0]['values'].count).to eq 1
    expect(body[:streams][0]['values'][0][0]).to eq '1546270458000000000'
    expect(body[:streams][0]['values'][0][1]).to eq Yajl.dump('message' => content[0])
  end

  it 'formats as simple string when only 1 record key' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      line_format json
      drop_single_key true
      <label>
        stream
      </label>
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog')
    line1 = [Time.at(1_546_270_458), { 'message' => content[0], 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['stream']).to eq('stream' => 'stdout')
    expect(body[:streams][0]['values'].count).to eq 1
    expect(body[:streams][0]['values'][0][0]).to eq '1546270458000000000'
    expect(body[:streams][0]['values'][0][1]).to eq content[0]
  end

  it 'order by timestamp then index when received unordered' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      drop_single_key true
      <label>
        stream
      </label>
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    lines = [
      [Time.at(1_546_270_460), { 'message' => '4', 'stream' => 'stdout' }],
      [Time.at(1_546_270_459), { 'message' => '2', 'stream' => 'stdout' }],
      [Time.at(1_546_270_458), { 'message' => '1', 'stream' => 'stdout' }],
      [Time.at(1_546_270_459), { 'message' => '3', 'stream' => 'stdout' }],
      [Time.at(1_546_270_450), { 'message' => '0', 'stream' => 'stdout' }],
      [Time.at(1_546_270_460), { 'message' => '5', 'stream' => 'stdout' }]
    ]
    res = driver.instance.generic_to_loki(lines)
    expect(res[0]['stream']).to eq('stream' => 'stdout')
    6.times { |i| expect(res[0]['values'][i][1]).to eq i.to_s }
  end

  it 'raises an LogPostError when http request is not successful' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    lines = [[Time.at(1_546_270_458), { 'message' => 'foobar', 'stream' => 'stdout' }]]

    # 200
    success = Net::HTTPSuccess.new(1.0, 200, 'OK')
    allow(driver.instance).to receive(:loki_http_request) { success }
    allow(success).to receive(:body).and_return('fake body')
    expect { driver.instance.write(lines) }.not_to raise_error

    # 205
    success = Net::HTTPSuccess.new(1.0, 205, 'OK')
    allow(driver.instance).to receive(:loki_http_request) { success }
    allow(success).to receive(:body).and_return('fake body')
    expect { driver.instance.write(lines) }.not_to raise_error

    # 429
    too_many_requests = Net::HTTPTooManyRequests.new(1.0, 429, 'OK')
    allow(driver.instance).to receive(:loki_http_request) { too_many_requests }
    allow(too_many_requests).to receive(:body).and_return('fake body')
    expect { driver.instance.write(lines) }.to raise_error(described_class::LogPostError)

    # 505
    server_error = Net::HTTPServerError.new(1.0, 505, 'OK')
    allow(driver.instance).to receive(:loki_http_request) { server_error }
    allow(server_error).to receive(:body).and_return('fake body')
    expect { driver.instance.write(lines) }.to raise_error(described_class::LogPostError)
  end

  context 'when output is multi-thread' do
    let(:thread) do
      class_double(
        'Thread',
        current: { _fluentd_plugin_helper_thread_title: 'thread1' }
      ).as_stubbed_const
    end

    before do
      allow(Thread).to receive(:new).and_yield(thread)
    end

    it 'adds the fluentd_label by default' do
      config = <<-CONF
        url https://logs-us-west1.grafana.net

        <buffer>
          @type memory
          flush_thread_count 2
        </buffer>
      CONF
      driver = Fluent::Test::Driver::Output.new(described_class)
      driver.configure(config)
      content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
      chunk = [Time.at(1_546_270_458), content[0]]
      payload = driver.instance.generic_to_loki([chunk])
      expect(payload[0]['stream']).to eq('fluentd_thread' => 'thread1')
    end

    it 'does not add the fluentd_label when configured' do
      config = <<-CONF
        url https://logs-us-west1.grafana.net
        include_thread_label  false

        <buffer>
          @type memory
          flush_thread_count 2
        </buffer>
      CONF
      driver = Fluent::Test::Driver::Output.new(described_class)
      driver.configure(config)
      content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
      chunk = [Time.at(1_546_270_458), content[0]]
      payload = driver.instance.generic_to_loki([chunk])
      expect(payload[0]['stream'].empty?).to eq(true)
    end
  end
end
