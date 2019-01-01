require 'spec_helper'
require 'logger'
require 'time'
require 'yajl'

RSpec.describe GenericToLoki do
  let(:logger) do
    logger = Logger.new(STDOUT)
    logger.level = Logger::DEBUG
  end

  it 'has a version number' do
    expect(Fluent::Plugin::VERSION).not_to be nil
  end

  it 'converts syslog output to loki output' do
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    single_chunk = [1_546_270_458, 'record' => content]
    extra_labels = {}
    payload = described_class.generic_to_loki(logger, extra_labels, [single_chunk])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T09:34:18.000000-06:00'
  end

  it 'converts syslog output with extra labels to loki output' do
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    single_chunk = [1_546_270_458, 'record' => content]
    extra_labels = { env: 'test' }
    payload = described_class.generic_to_loki(logger, extra_labels, [single_chunk])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{env="test"}'
    expect(body[:streams][0]['entries'].count).to eq 1
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T09:34:18.000000-06:00'
  end

  it 'converts multiple syslog output lines to loki output' do
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    line1 = [1_546_270_458, 'record' => content[0]]
    line2 = [1_546_270_460, 'record' => content[1]]
    extra_labels = {}
    payload = described_class.generic_to_loki(logger, extra_labels, [line1, line2])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{}'
    expect(body[:streams][0]['entries'].count).to eq 2
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T09:34:18.000000-06:00'
    expect(body[:streams][0]['entries'][1]['ts']).to eq '2018-12-31T09:34:20.000000-06:00'
  end

  it 'converts multiple syslog output lines with extra labels to loki output' do
    content = File.readlines('spec/gems/fluent/plugin/data/syslog2')
    line1 = [1_546_270_458, 'record' => content[0]]
    line2 = [1_546_270_460, 'record' => content[1]]
    extra_labels = { env: 'test' }
    payload = described_class.generic_to_loki(logger, extra_labels, [line1, line2])
    body = { 'streams': payload }
    expect(body[:streams][0]['labels']).to eq '{env="test"}'
    expect(body[:streams][0]['entries'].count).to eq 2
    expect(body[:streams][0]['entries'][0]['ts']).to eq '2018-12-31T09:34:18.000000-06:00'
    expect(body[:streams][0]['entries'][1]['ts']).to eq '2018-12-31T09:34:20.000000-06:00'
  end
end
