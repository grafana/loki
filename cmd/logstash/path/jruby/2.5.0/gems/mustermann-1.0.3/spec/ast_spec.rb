# frozen_string_literal: true
require 'support'
require 'mustermann/ast/node'

describe Mustermann::AST do
  describe :type do
    example { Mustermann::AST::Node[:char].type     .should be == :char }
    example { Mustermann::AST::Node[:char].new.type .should be == :char }
  end

  describe :min_size do
    example { Mustermann::AST::Node[:char].new.min_size.should be == 1 }
    example { Mustermann::AST::Node[:node].new.min_size.should be == 0 }
  end
end
