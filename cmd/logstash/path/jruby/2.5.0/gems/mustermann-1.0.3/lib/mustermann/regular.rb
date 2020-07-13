# frozen_string_literal: true
require 'mustermann'
require 'mustermann/regexp_based'
require 'strscan'

module Mustermann
  # Regexp pattern implementation.
  #
  # @example
  #   Mustermann.new('/.*', type: :regexp) === '/bar' # => true
  #
  # @see Mustermann::Pattern
  # @see file:README.md#simple Syntax description in the README
  class Regular < RegexpBased
    include Concat::Native
    register :regexp, :regular
    supported_options :check_anchors

    # @param (see Mustermann::Pattern#initialize)
    # @return (see Mustermann::Pattern#initialize)
    # @see (see Mustermann::Pattern#initialize)
    def initialize(string, check_anchors: true, **options)
      string = $1 if string.to_s =~ /\A\(\?\-mix\:(.*)\)\Z/ && string.inspect == "/#$1/"
      string = string.source.gsub!(/(?<!\\)(?:\s|#.*$)/, '') if extended_regexp?(string)
      @check_anchors = check_anchors
      super(string, **options)
    end

    def compile(**options)
      if @check_anchors
        scanner = ::StringScanner.new(@string)
        check_anchors(scanner) until scanner.eos?
      end

      /#{@string}/
    end

    def check_anchors(scanner)
      return scanner.scan_until(/\]/) if scanner.scan(/\[/)
      return scanner.scan(/\\?./) unless illegal = scanner.scan(/\\[AzZ]|[\^\$]/)
      raise CompileError, "regular expression should not contain %s: %p" % [illegal.to_s, @string]
    end

    def extended_regexp?(string)
      not (Regexp.new(string).options & Regexp::EXTENDED).zero?
    end

    private :compile, :check_anchors, :extended_regexp?
  end
end
