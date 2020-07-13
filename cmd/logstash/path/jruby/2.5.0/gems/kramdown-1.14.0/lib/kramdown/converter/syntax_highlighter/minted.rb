# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown::Converter::SyntaxHighlighter

  # Uses Minted to highlight code blocks and code spans.
  module Minted

    def self.call(converter, text, lang, type, _opts)
      opts = converter.options[:syntax_highlighter_opts]

      # Fallback to default language
      lang ||= opts[:default_lang]

      options = []
      options << "breaklines" if opts[:wrap]
      options << "linenos" if opts[:line_numbers]
      options << "frame=#{opts[:frame]}" if opts[:frame]

      if lang && type == :block
        "\\begin{minted}[#{options.join(',')}]{#{lang}}\n#{text}\n\\end{minted}"
      elsif lang && type == :span
        "\\mintinline{#{lang}}{#{text}}"
      else
        nil
      end
    end
  end
end
