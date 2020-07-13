# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#
# = kramdown
#
# kramdown is fast, pure Ruby Markdown superset converter, using a strict syntax definition and
# supporting several common extensions.
#
# The kramdown library is mainly written to support the kramdown-to-HTML conversion chain. However,
# due to its flexibility it supports other input and output formats as well. Here is a list of the
# supported formats:
#
# * input formats: kramdown (a Markdown superset), Markdown, GFM, HTML
# * output formats: HTML, kramdown, LaTeX (and therefore PDF), PDF via Prawn
#
# All the documentation on the available input and output formats is available at
# http://kramdown.gettalong.org.
#
# == Usage
#
# kramdown has a simple API, so using kramdown is as easy as
#
#     require 'kramdown'
#
#     Kramdown::Document.new(text).to_html
#
# For detailed information have a look at the *\Kramdown::Document* class.
#
# == License
#
# MIT - see the COPYING file.


require 'kramdown/compatibility'

require 'kramdown/version'
require 'kramdown/element'
require 'kramdown/error'
require 'kramdown/parser'
require 'kramdown/converter'
require 'kramdown/options'
require 'kramdown/utils'

module Kramdown

  # Return the data directory for kramdown.
  def self.data_dir
    unless defined?(@@data_dir)
      require 'rbconfig'
      @@data_dir = File.expand_path(File.join(File.dirname(__FILE__), '..', '..', 'data', 'kramdown'))
      @@data_dir = File.expand_path(File.join(RbConfig::CONFIG["datadir"], "kramdown")) if !File.exists?(@@data_dir)
      raise "kramdown data directory not found! This is a bug, please report it!" unless File.directory?(@@data_dir)
    end
    @@data_dir
  end


  # The main interface to kramdown.
  #
  # This class provides a one-stop-shop for using kramdown to convert text into various output
  # formats. Use it like this:
  #
  #   require 'kramdown'
  #   doc = Kramdown::Document.new('This *is* some kramdown text')
  #   puts doc.to_html
  #
  # The #to_html method is a shortcut for using the Converter::Html class. See #method_missing for
  # more information.
  #
  # The second argument to the ::new method is an options hash for customizing the behaviour of the
  # used parser and the converter. See ::new for more information!
  class Document

    # The root Element of the element tree. It is immediately available after the ::new method has
    # been called.
    attr_accessor :root

    # The options hash which holds the options for parsing/converting the Kramdown document.
    attr_reader :options

    # An array of warning messages. It is filled with warnings during the parsing phase (i.e. in
    # ::new) and the conversion phase.
    attr_reader :warnings


    # Create a new Kramdown document from the string +source+ and use the provided +options+. The
    # options that can be used are defined in the Options module.
    #
    # The special options key :input can be used to select the parser that should parse the
    # +source+. It has to be the name of a class in the Kramdown::Parser module. For example, to
    # select the kramdown parser, one would set the :input key to +Kramdown+. If this key is not
    # set, it defaults to +Kramdown+.
    #
    # The +source+ is immediately parsed by the selected parser so that the root element is
    # immediately available and the output can be generated.
    def initialize(source, options = {})
      @options = Options.merge(options).freeze
      parser = (@options[:input] || 'kramdown').to_s
      parser = parser[0..0].upcase + parser[1..-1]
      try_require('parser', parser)
      if Parser.const_defined?(parser)
        @root, @warnings = Parser.const_get(parser).parse(source, @options)
      else
        raise Kramdown::Error.new("kramdown has no parser to handle the specified input format: #{@options[:input]}")
      end
    end

    # Check if a method is invoked that begins with +to_+ and if so, try to instantiate a converter
    # class (i.e. a class in the Kramdown::Converter module) and use it for converting the document.
    #
    # For example, +to_html+ would instantiate the Kramdown::Converter::Html class.
    def method_missing(id, *attr, &block)
      if id.to_s =~ /^to_(\w+)$/ && (name = Utils.camelize($1)) &&
          try_require('converter', name) && Converter.const_defined?(name)
        output, warnings = Converter.const_get(name).convert(@root, @options)
        @warnings.concat(warnings)
        output
      else
        super
      end
    end

    def inspect #:nodoc:
      "<KD:Document: options=#{@options.inspect} root=#{@root.inspect} warnings=#{@warnings.inspect}>"
    end

    # Try requiring a parser or converter class and don't raise an error if the file is not found.
    def try_require(type, name)
      require("kramdown/#{type}/#{Utils.snake_case(name)}")
      true
    rescue LoadError
      true
    end
    protected :try_require

  end

end

