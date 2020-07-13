# frozen_string_literal: true

require 'rack/utils'

RSpec.describe Faraday::FlatParamsEncoder do
  it_behaves_like 'a params encoder'

  it 'decodes arrays' do
    query = 'a=one&a=two&a=three'
    expected = { 'a' => %w[one two three] }
    expect(subject.decode(query)).to eq(expected)
  end

  it 'decodes boolean values' do
    query = 'a=true&b=false'
    expected = { 'a' => 'true', 'b' => 'false' }
    expect(subject.decode(query)).to eq(expected)
  end

  it 'encodes boolean values' do
    params = { a: true, b: false }
    expect(subject.encode(params)).to eq('a=true&b=false')
  end

  it 'encodes boolean values in array' do
    params = { a: [true, false] }
    expect(subject.encode(params)).to eq('a=true&a=false')
  end

  it 'encodes empty array in hash' do
    params = { a: [] }
    expect(subject.encode(params)).to eq('a=')
  end
end
