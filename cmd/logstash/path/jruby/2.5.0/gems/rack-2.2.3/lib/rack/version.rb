# frozen_string_literal: true

# Copyright (C) 2007-2019 Leah Neukirchen <http://leahneukirchen.org/infopage.html>
#
# Rack is freely distributable under the terms of an MIT-style license.
# See MIT-LICENSE or https://opensource.org/licenses/MIT.

# The Rack main module, serving as a namespace for all core Rack
# modules and classes.
#
# All modules meant for use in your application are <tt>autoload</tt>ed here,
# so it should be enough just to <tt>require 'rack'</tt> in your code.

module Rack
  # The Rack protocol version number implemented.
  VERSION = [1, 3]

  # Return the Rack protocol version as a dotted string.
  def self.version
    VERSION.join(".")
  end

  RELEASE = "2.2.3"

  # Return the Rack release as a dotted string.
  def self.release
    RELEASE
  end
end
