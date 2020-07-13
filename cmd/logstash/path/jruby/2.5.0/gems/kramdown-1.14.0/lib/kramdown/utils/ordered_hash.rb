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

    if RUBY_VERSION < '1.9'

      # A partial hash implementation which preserves the insertion order of the keys.
      #
      # *Note* that this class is only used on Ruby 1.8 since the built-in Hash on Ruby 1.9
      # automatically preserves the insertion order. However, to remain compatibility only the
      # methods defined in this class may be used when working with OrderedHash on Ruby 1.9.
      class OrderedHash

        include Enumerable

        # Initialize the OrderedHash object.
        def initialize
          @data =  {}
          @order = []
        end

        # Iterate over the stored keys in insertion order.
        def each
          @order.each {|k| yield(k, @data[k])}
        end

        # Return the value for the +key+.
        def [](key)
          @data[key]
        end

        # Return +true+ if the hash contains the key.
        def has_key?(key)
          @data.has_key?(key)
        end

        # Return +true+ if the hash contains no keys.
        def empty?
          @data.empty?
        end

        # Set the value for the +key+ to +val+.
        def []=(key, val)
          @order << key if !@data.has_key?(key)
          @data[key] = val
        end

        # Delete the +key+.
        def delete(key)
          @order.delete(key)
          @data.delete(key)
        end

        def merge!(other)
          other.each {|k,v| self[k] = v}
          self
        end

        def dup #:nodoc:
          new_object = super
          new_object.instance_variable_set(:@data, @data.dup)
          new_object.instance_variable_set(:@order, @order.dup)
          new_object
        end

        def ==(other) #:nodoc:
          return false unless other.kind_of?(self.class)
          @data == other.instance_variable_get(:@data) && @order == other.instance_variable_get(:@order)
        end

        def inspect #:nodoc:
          "{" + map {|k,v| "#{k.inspect}=>#{v.inspect}"}.join(" ") + "}"
        end

      end

    else
      OrderedHash = Hash
    end

  end

end
