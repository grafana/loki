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
  end

  it 'converts syslog output to loki output' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    single_chunk = [1_546_270_458, content]
    payload = driver.instance.generic_to_loki([single_chunk])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
  end

  it 'converts syslog output with extra labels to loki output' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      extra_labels {"env": "test"}
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    single_chunk = [1_546_270_458, content]
    payload = driver.instance.generic_to_loki([single_chunk])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{env="test"}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
  end

  it 'converts multiple syslog output lines to loki output' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    line1 = [1_546_270_458, content[0]]
    line2 = [1_546_270_460, content[1]]
    payload = driver.instance.generic_to_loki([line1, line2])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{}'
    expect(body[:streams][0]['entries'].count).to eq 2
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
    expect(body[:streams][0]['entries'][1]['ts']).to eq '2018-12-31T15:34:20.000000Z'
  end

  it 'converts multiple syslog output lines with extra labels to loki output' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      extra_labels {"env": "test"}
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    line1 = [1_546_270_458, content[0]]
    line2 = [1_546_270_460, content[1]]
    payload = driver.instance.generic_to_loki([line1, line2])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{env="test"}'
    expect(body[:streams][0]['entries'].count).to eq 2
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
    expect(body[:streams][0]['entries'][1]['ts']).to eq '2018-12-31T15:34:20.000000Z'
  end

  it 'formats record hash as key_value' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog')
    line1 = [1_546_270_458, { 'message' => content[0], 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
    expect(body[:streams][0]['entries'][0]['line']).to eq 'message="' + content[0] + '" stream="stdout"'
  end

  it 'formats record hash as json' do
    config = <<-CONF
      url     https://logs-us-west1.grafana.net
      line_format json
    CONF
    driver = Fluent::Test::Driver::Output.new(described_class)
    driver.configure(config)
    content = File.readlines('spec/gems/fluent/plugin/data/syslog')
    line1 = [1_546_270_458, { 'message' => content[0], 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
    expect(body[:streams][0]['entries'][0]['line']).to eq Yajl.dump(line1[1])
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
    line1 = [1_546_270_458, { 'message' => content[0], 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{stream="stdout"}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
    expect(body[:streams][0]['entries'][0]['line']).to eq Yajl.dump('message' => content[0])
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
    line1 = [1_546_270_458, { 'message' => content[0], 'kubernetes' => { 'pod' => 'podname' } }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{pod="podname"}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
    expect(body[:streams][0]['entries'][0]['line']).to eq Yajl.dump('message' => content[0], 'kubernetes' => {})
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
    line1 = [1_546_270_458, { 'message' => content[0], 'kubernetes' => { 'pod' => 'podname' } }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{pod="podname"}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
    expect(body[:streams][0]['entries'][0]['line']).to eq Yajl.dump('message' => content[0])
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
    line1 = [1_546_270_458, { 'message' => content[0], 'stream' => 'stdout' }]
    payload = driver.instance.generic_to_loki([line1])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{stream="stdout"}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T15:34:18.000000Z'
    expect(body[:streams][0]['entries'][0]['line']).to eq content[0]
  end
end
