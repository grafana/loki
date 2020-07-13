# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown::Converter::SyntaxHighlighter

  # Uses Coderay to highlight code blocks and code spans.
  module Coderay

    begin
      require 'coderay'

      # Highlighting via coderay is available if this constant is +true+.
      AVAILABLE = true
    rescue LoadError
      AVAILABLE = false  # :nodoc:
    end

    def self.call(converter, text, lang, type, call_opts)
      return nil unless converter.options[:enable_coderay]

      if type == :span && lang
        ::CodeRay.scan(text, lang.to_sym).html(options(converter, :span)).chomp
      elsif type == :block && (lang || options(converter, :default_lang))
        lang ||= call_opts[:default_lang] = options(converter, :default_lang)
        ::CodeRay.scan(text, lang.to_s.gsub(/-/, '_').to_sym).html(options(converter, :block)).chomp << "\n"
      else
        nil
      end
    rescue
      converter.warning("There was an error using CodeRay: #{$!.message}")
      nil
    end

    def self.options(converter, type)
      prepare_options(converter)
      converter.data[:syntax_highlighter_coderay][type]
    end

    def self.prepare_options(converter)
      return if converter.data.key?(:syntax_highlighter_coderay)

      cache = converter.data[:syntax_highlighter_coderay] = {}

      opts = converter.options[:syntax_highlighter_opts].dup
      span_opts = (opts.delete(:span) || {}).dup
      block_opts = (opts.delete(:block) || {}).dup
      [span_opts, block_opts].each do |hash|
        hash.keys.each do |k|
          hash[k.kind_of?(String) ? Kramdown::Options.str_to_sym(k) : k] = hash.delete(k)
        end
      end

      cache[:default_lang] = converter.options[:coderay_default_lang] || opts.delete(:default_lang)
      cache[:span] = {
        :css => converter.options[:coderay_css]
      }.update(opts).update(span_opts).update(:wrap => :span)
      cache[:block] = {
        :wrap => converter.options[:coderay_wrap],
        :line_numbers => converter.options[:coderay_line_numbers],
        :line_number_start => converter.options[:coderay_line_number_start],
        :tab_width => converter.options[:coderay_tab_width],
        :bold_every => converter.options[:coderay_bold_every],
        :css => converter.options[:coderay_css]
      }.update(opts).update(block_opts)

      [:css, :wrap, :line_numbers].each do |key|
        [:block, :span].each do |type|
          cache[type][key] = cache[type][key].to_sym if cache[type][key].kind_of?(String)
        end
      end
    end

  end

end
