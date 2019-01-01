require 'spec_helper'
require 'logger'
require 'time'
require 'yajl'

RSpec.describe ConversionHelpers do
  logger = Logger.new(STDOUT)
  logger.level = Logger::INFO

  it 'has a version number' do
    expect(Fluent::Plugin::VERSION).not_to be nil
  end
  it 'converts simple line to loki output' do
    record = 'Test 123'
    result = described_class.line_to_loki(logger, record)
    expect(result[:line]).to eq 'Test 123'
  end

  it 'converts a chunk to loki output' do
    single_chunk = [1_546_270_458, 'message' => 'Simple Chunk Text']
    result = described_class.chunk_to_loki(logger, [single_chunk])
    result.each do |_, v|
      expect(v[0]['ts']).to eq '2018-12-31T09:34:18.000000-06:00'
      expect(v[0]['line']).to eq 'Simple Chunk Text'
    end
  end
end
