# frozen_string_literal: true
require 'support'
require 'mustermann/regexp_based'

describe Mustermann::RegexpBased do
  it 'raises a NotImplementedError when used directly' do
    expect { Mustermann::RegexpBased.new("") === "" }.to raise_error(NotImplementedError)
  end
end
