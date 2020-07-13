# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown::Converter::SyntaxHighlighter

  # Uses Rouge which is CSS-compatible to Pygments to highlight code blocks and code spans.
  module Rouge

    begin
      require 'rouge'

      # Highlighting via Rouge is available if this constant is +true+.
      AVAILABLE = true

      begin
        # Rouge::Formatters::HTMLLegacy is available on Rouge 2.0 or later
        FORMATTER_CLASS = ::Rouge::Formatters::HTMLLegacy
      rescue NameError
        # Fallbacks to Rouge 1.x formatter if Rouge::Formatters::HTMLLegacy is not available
        FORMATTER_CLASS = ::Rouge::Formatters::HTML
      end
    rescue LoadError, SyntaxError
      AVAILABLE = false  # :nodoc:
    end

    def self.call(converter, text, lang, type, call_opts)
      opts = options(converter, type)
      call_opts[:default_lang] = opts[:default_lang]
      lexer = ::Rouge::Lexer.find_fancy(lang || opts[:default_lang], text)
      return nil if opts[:disable] || !lexer
      opts[:css_class] ||= 'highlight' # For backward compatibility when using Rouge 2.0
      formatter = (opts.fetch(:formatter, FORMATTER_CLASS)).new(opts)
      formatter.format(lexer.lex(text))
    end

    def self.options(converter, type)
      prepare_options(converter)
      converter.data[:syntax_highlighter_rouge][type]
    end

    def self.prepare_options(converter)
      return if converter.data.key?(:syntax_highlighter_rouge)

      cache = converter.data[:syntax_highlighter_rouge] = {}

      opts = converter.options[:syntax_highlighter_opts].dup
      span_opts = (opts.delete(:span) || {}).dup
      block_opts = (opts.delete(:block) || {}).dup
      [span_opts, block_opts].each do |hash|
        hash.keys.each do |k|
          hash[k.kind_of?(String) ? Kramdown::Options.str_to_sym(k) : k] = hash.delete(k)
        end
      end

      cache[:span] = opts.merge(span_opts).update(:wrap => false)
      cache[:block] = opts.merge(block_opts)
    end

  end

end
