# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown

  # This error is raised when an error condition is encountered.
  #
  # *Note* that this error is only raised by the support framework for the parsers and converters.
  class Error < RuntimeError; end

end
