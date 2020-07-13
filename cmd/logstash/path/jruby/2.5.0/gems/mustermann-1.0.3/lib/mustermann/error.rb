# frozen_string_literal: true
module Mustermann
  unless defined?(Mustermann::Error)
    Error        = Class.new(StandardError) # Raised if anything goes wrong while generating a {Pattern}.
    CompileError = Class.new(Error)         # Raised if anything goes wrong while compiling a {Pattern}.
    ParseError   = Class.new(Error)         # Raised if anything goes wrong while parsing a {Pattern}.
    ExpandError  = Class.new(Error)         # Raised if anything goes wrong while expanding a {Pattern}.
  end
end
