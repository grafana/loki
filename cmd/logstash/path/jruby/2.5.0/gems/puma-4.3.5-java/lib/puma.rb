# frozen_string_literal: true

# Standard libraries
require 'socket'
require 'tempfile'
require 'time'
require 'etc'
require 'uri'
require 'stringio'

require 'thread'

module Puma
  autoload :Const, 'puma/const'
  autoload :Server, 'puma/server'
  autoload :Launcher, 'puma/launcher'

  def self.stats_object=(val)
    @get_stats = val
  end

  def self.stats
    @get_stats.stats
  end

  # Thread name is new in Ruby 2.3
  def self.set_thread_name(name)
    return unless Thread.current.respond_to?(:name=)
    Thread.current.name = "puma #{name}"
  end
end
