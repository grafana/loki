# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown

  # This module contains all available parsers. A parser takes an input string and converts the
  # string to an element tree.
  #
  # New parsers should be derived from the Base class which provides common functionality - see its
  # API documentation for how to create a custom converter class.
  module Parser

    autoload :Base, 'kramdown/parser/base'
    autoload :Kramdown, 'kramdown/parser/kramdown'
    autoload :Html, 'kramdown/parser/html'
    autoload :Markdown, 'kramdown/parser/markdown'
    autoload :GFM, 'kramdown/parser/gfm'

  end

end
