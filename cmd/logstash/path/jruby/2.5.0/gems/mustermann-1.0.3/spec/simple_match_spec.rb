# frozen_string_literal: true
require 'support'
require 'mustermann/simple_match'

describe Mustermann::SimpleMatch do
  subject { Mustermann::SimpleMatch.new('example') }
  its(:to_s) { should be == 'example' }
  its(:names) { should be == [] }
  its(:captures) { should be == [] }
  example { subject[1].should be == nil }
end
