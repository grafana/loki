# frozen_string_literal: true
require 'support'
require 'mustermann/to_pattern'
require 'delegate'

describe Mustermann::ToPattern do
  context String do
    example { "".to_pattern               .should be_a(Mustermann::Sinatra) }
    example { "".to_pattern(type: :rails) .should be_a(Mustermann::Rails)   }
  end

  context Regexp do
    example { //.to_pattern               .should be_a(Mustermann::Regular) }
    example { //.to_pattern(type: :rails) .should be_a(Mustermann::Regular) }
  end

  context Symbol do
    example { :foo.to_pattern               .should be_a(Mustermann::Sinatra) }
    example { :foo.to_pattern(type: :rails) .should be_a(Mustermann::Sinatra) }
  end

  context Array do
    example { [:foo, :bar].to_pattern               .should be_a(Mustermann::Composite) }
    example { [:foo, :bar].to_pattern(type: :rails) .should be_a(Mustermann::Composite) }
  end

  context Mustermann::Pattern do
    subject(:pattern) { Mustermann.new('') }
    example { pattern.to_pattern.should be == pattern }
    example { pattern.to_pattern(type: :rails).should be_a(Mustermann::Sinatra) }
  end

  context 'custom class' do
    let(:example_class) do
      Class.new do
        include Mustermann::ToPattern
        def to_s
          ":foo/:bar"
        end
      end
    end

    example { example_class.new.to_pattern                    .should be_a(Mustermann::Sinatra) }
    example { example_class.new.to_pattern(type: :rails)      .should be_a(Mustermann::Rails)   }
    example { Mustermann.new(example_class.new)               .should be_a(Mustermann::Sinatra) }
    example { Mustermann.new(example_class.new, type: :rails) .should be_a(Mustermann::Rails)   }
  end

  context 'primitive delegate' do
    let(:example_class) do
      Class.new(DelegateClass(Array)) do
        include Mustermann::ToPattern
      end
    end

    example { example_class.new([:foo, :bar]).to_pattern               .should be_a(Mustermann::Composite) }
    example { example_class.new([:foo, :bar]).to_pattern(type: :rails) .should be_a(Mustermann::Composite) }
  end

  context 'primitive subclass' do
    let(:example_class) do
      Class.new(Array) do
        include Mustermann::ToPattern
      end
    end

    example { example_class.new([:foo, :bar]).to_pattern               .should be_a(Mustermann::Composite) }
    example { example_class.new([:foo, :bar]).to_pattern(type: :rails) .should be_a(Mustermann::Composite) }
  end
end
