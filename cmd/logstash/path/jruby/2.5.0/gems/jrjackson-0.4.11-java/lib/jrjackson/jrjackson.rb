unless RUBY_PLATFORM =~ /java/
  puts "This library is only compatible with a java-based ruby environment like JRuby."
  exit 255
end

require "jrjackson_jars"
require_relative "build_info"

require_relative "jars/jrjackson-#{JrJackson::BuildInfo.jar_version}.jar"

require 'com/jrjackson/jr_jackson'
require 'bigdecimal'

module JrJackson
  module Json
      class << self
      TIME_REGEX = %r(\A(\d{4}-\d\d-\d\d|(\w{3}\s){2}\d\d)\s\d\d:\d\d:\d\d)

      def sj_load(handler, json_source, options = nil)
        JrJackson::Saj.parse(handler, json_source, options)
      end

      def sc_load(handler, json_source, options = nil)
        JrJackson::Sch.parse(handler, json_source, options)
      end

      def load_ruby(json_source, options = nil)
        JrJackson::Ruby.parse(json_source, options)
      end

      def load_java(json_source, options = nil)
        JrJackson::Java.parse(json_source, options)
      end

      def load(json_source, options = nil)
        if json_source.is_a?(String) && is_time_string?(json_source)
          return JrJackson::Raw.parse_raw("\"#{json_source}\"")
        end

        if options && !options.empty?
          if options.size == 1 && !!options[:raw]
            return JrJackson::Java.parse_raw(json_source, options)
          end
          if options.size == 1 && !!options[:symbolize_keys]
            return JrJackson::Ruby.parse_sym(json_source, options)
          end
          if options.size == 2 && !!options[:raw] && !!options[:use_bigdecimal]
            return JrJackson::Java.parse(json_source, options)
          end
          # JrJackson::Raw.parse(json_source, options)
          JrJackson::Ruby.parse(json_source, options)
        else
          # JrJackson::Raw.parse_str(json_source)
          JrJackson::Ruby.parse(json_source, nil)
        end
      end

      def dump(object, options = {})
        case object
        when Hash, Array, String, ::Java::JavaUtil::Map, ::Java::JavaUtil::List
          JrJackson::Base.generate(object, options)
        when true, false
          object.to_s
        when nil
          'null'
        else
          if object.respond_to?(:to_json_data)
            JrJackson::Base.generate(object, options)
          elsif object.respond_to?(:to_json)
            object.to_json
          elsif object.respond_to?(:to_s)
            object.to_s
          else
            object
          end
        end
      end

      alias :sc_parse :sc_load
      alias :sj_parse :sj_load
      alias :parse :load
      alias :parse_ruby :load_ruby
      alias :parse_java :load_java
      alias :generate :dump

      private

      def is_time_string?(json_source)
        json_source =~ TIME_REGEX
      rescue => e
        raise JrJackson::ParseError, e.message
      end
    end
  end
end
