# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown
  module Utils

    # Methods for registering configurable extensions.
    module Configurable

      # Create a new configurable extension called +name+.
      #
      # Three methods will be defined on the calling object which allow to use this configurable
      # extension:
      #
      # configurables:: Returns a hash of hashes that is used to store all configurables of the
      #                 object.
      #
      # <name>(ext_name):: Return the configured extension +ext_name+.
      #
      # add_<name>(ext_name, data=nil, &block):: Define an extension +ext_name+ by specifying either
      #                                          the data as argument or by using a block.
      def configurable(name)
        singleton_class = (class << self; self; end)
        singleton_class.send(:define_method, :configurables) do
          @_configurables ||= Hash.new {|h, k| h[k] = {}}
        end unless respond_to?(:configurables)
        singleton_class.send(:define_method, name) do |data|
          configurables[name][data]
        end
        singleton_class.send(:define_method, "add_#{name}".intern) do |data, *args, &block|
          configurables[name][data] = args.first || block
        end
      end

    end

  end
end
