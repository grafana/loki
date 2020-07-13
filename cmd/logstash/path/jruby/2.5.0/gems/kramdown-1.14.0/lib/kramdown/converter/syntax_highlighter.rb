# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown
  module Converter

    # == Container for Syntax Highlighters
    #
    # This module serves as container for the syntax highlighters that can be used together with
    # kramdown.
    #
    # A syntax highlighter should not store any data itself but should use the provided converter
    # object to do so (See Kramdown::Converter::Base#data).
    #
    # == Implementing a Syntax Highlighter
    #
    # Implementing a new syntax highlighter is easy because it is just an object that needs to
    # respond to #call.
    #
    # The method #call needs to take the following arguments:
    #
    # converter:: This argument contains the converter object that calls the syntax highlighter. It
    #             can be used, for example, to store data in Kramdown::Converter::Base#data for one
    #             conversion run.
    #
    # text:: The raw text that should be highlighted.
    #
    # lang:: The language that the text should be highlighted for (e.g. ruby, python, ...).
    #
    # type:: The type of text, either :span for span-level code or :block for a codeblock.
    #
    # opts:: A Hash with options that may be passed from the converter.
    #
    # The return value of the method should be the highlighted text, suitable for the given
    # converter (e.g. HTML for the HTML converter).
    #
    # == Special Implementation Details
    #
    # HTML converter:: If the syntax highlighter is used with a HTML converter, it should return
    #                  :block type text correctly wrapped (i.e. normally inside a pre-tag, but may
    #                  also be a table-tag or just a div-tag) but :span type text *without* a
    #                  code-tag!
    #
    #                  Also, a syntax highlighter should store the default highlighting language for
    #                  the invocation in the +opts+ hash under the key :default_lang.
    module SyntaxHighlighter
    end

  end
end
