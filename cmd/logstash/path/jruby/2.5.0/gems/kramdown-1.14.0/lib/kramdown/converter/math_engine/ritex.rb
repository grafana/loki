# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown::Converter::MathEngine

  # Uses the Ritex library for converting math formulas to MathML.
  module Ritex

    begin
      require 'ritex'

      # Ritex is available if this constant is +true+.
      AVAILABLE = true
    rescue LoadError
      AVAILABLE = false  # :nodoc:
    end

    def self.call(converter, el, opts)
      type = el.options[:category]
      result = ::Ritex::Parser.new.parse(el.value, :display => (type == :block))

      attr = el.attr.dup
      attr.delete('xmlns')
      attr.delete('display')
      result.insert("<math".length, converter.html_attributes(attr))

      (type == :block ? "#{' '*opts[:indent]}#{result}\n" : result)
    end

  end

end
