# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown::Converter::MathEngine

  # Uses the Itex2MML library for converting math formulas to MathML.
  module Itex2MML

    begin
      require 'itextomml'

      # Itex2MML is available if this constant is +true+.
      AVAILABLE = true
    rescue LoadError
      AVAILABLE = false  # :nodoc:
    end

    def self.call(converter, el, opts)
      type = el.options[:category]
      parser = ::Itex2MML::Parser.new
      result = (type == :block ? parser.block_filter(el.value) : parser.inline_filter(el.value))

      attr = el.attr.dup
      attr.delete('xmlns')
      attr.delete('display')
      result.insert("<math".length, converter.html_attributes(attr))

      (type == :block ? "#{' '*opts[:indent]}#{result}\n" : result)
    end

  end

end
