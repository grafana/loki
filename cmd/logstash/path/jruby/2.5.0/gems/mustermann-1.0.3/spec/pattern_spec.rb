# frozen_string_literal: true
require 'support'
require 'mustermann/pattern'
require 'mustermann/sinatra'
require 'mustermann/rails'

describe Mustermann::Pattern do
  describe :=== do
    it 'raises a NotImplementedError when used directly' do
      expect { Mustermann::Pattern.new("") === "" }.to raise_error(NotImplementedError)
    end
  end

  describe :initialize do
    it 'raises an ArgumentError for unknown options' do
      expect { Mustermann::Pattern.new("", foo: :bar) }.to raise_error(ArgumentError)
    end

    it 'does not complain about unknown options if ignore_unknown_options is enabled' do
      expect { Mustermann::Pattern.new("", foo: :bar, ignore_unknown_options: true) }.not_to raise_error
    end
  end

  describe :respond_to? do
    subject(:pattern) { Mustermann::Pattern.new("") }

    it { should_not respond_to(:expand)       }
    it { should_not respond_to(:to_templates) }

    it { expect { pattern.expand }       .to raise_error(NotImplementedError) }
    it { expect { pattern.to_templates } .to raise_error(NotImplementedError) }
  end

  describe :== do
    example { Mustermann::Pattern.new('/foo') .should     be == Mustermann::Pattern.new('/foo') }
    example { Mustermann::Pattern.new('/foo') .should_not be == Mustermann::Pattern.new('/bar') }
    example { Mustermann::Sinatra.new('/foo') .should     be == Mustermann::Sinatra.new('/foo') }
    example { Mustermann::Rails.new('/foo')   .should_not be == Mustermann::Sinatra.new('/foo') }
  end

  describe :eql? do
    example { Mustermann::Pattern.new('/foo') .should     be_eql Mustermann::Pattern.new('/foo') }
    example { Mustermann::Pattern.new('/foo') .should_not be_eql Mustermann::Pattern.new('/bar') }
    example { Mustermann::Sinatra.new('/foo') .should     be_eql Mustermann::Sinatra.new('/foo') }
    example { Mustermann::Rails.new('/foo')   .should_not be_eql Mustermann::Sinatra.new('/foo') }
  end

  describe :equal? do
    example { Mustermann::Pattern.new('/foo') .should     be_equal Mustermann::Pattern.new('/foo') }
    example { Mustermann::Pattern.new('/foo') .should_not be_equal Mustermann::Pattern.new('/bar') }
    example { Mustermann::Sinatra.new('/foo') .should     be_equal Mustermann::Sinatra.new('/foo') }
    example { Mustermann::Rails.new('/foo')   .should_not be_equal Mustermann::Sinatra.new('/foo') }
  end
end
