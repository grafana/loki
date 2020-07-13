# frozen_string_literal: true

require 'multipart_parser/reader'

module Faraday
  module HelperMethods
    def self.included(base)
      base.extend ClassMethods
    end

    module ClassMethods
      def features(*features)
        @features = features
      end

      def on_feature(name)
        yield if block_given? && feature?(name)
      end

      def feature?(name)
        if @features.nil?
          superclass.feature?(name) if superclass.respond_to?(:feature?)
        elsif @features.include?(name)
          true
        end
      end

      def method_with_body?(method)
        METHODS_WITH_BODY.include?(method.to_s)
      end
    end

    def ssl_mode?
      ENV['SSL'] == 'yes'
    end

    def normalize(url)
      Faraday::Utils::URI(url)
    end

    def with_default_uri_parser(parser)
      old_parser = Faraday::Utils.default_uri_parser
      begin
        Faraday::Utils.default_uri_parser = parser
        yield
      ensure
        Faraday::Utils.default_uri_parser = old_parser
      end
    end

    def with_env(new_env)
      old_env = {}

      new_env.each do |key, value|
        old_env[key] = ENV.fetch(key, false)
        ENV[key] = value
      end

      begin
        yield
      ensure
        old_env.each do |key, value|
          value == false ? ENV.delete(key) : ENV[key] = value
        end
      end
    end

    def with_env_proxy_disabled
      Faraday.ignore_env_proxy = true

      begin
        yield
      ensure
        Faraday.ignore_env_proxy = false
      end
    end

    def capture_warnings
      old = $stderr
      $stderr = StringIO.new
      begin
        yield
        $stderr.string
      ensure
        $stderr = old
      end
    end

    def multipart_file
      Faraday::FilePart.new(__FILE__, 'text/x-ruby')
    end

    # parse boundary out of a Content-Type header like:
    #   Content-Type: multipart/form-data; boundary=gc0p4Jq0M2Yt08jU534c0p
    def parse_multipart_boundary(ctype)
      MultipartParser::Reader.extract_boundary_value(ctype)
    end

    # parse a multipart MIME message, returning a hash of any multipart errors
    def parse_multipart(boundary, body)
      reader = MultipartParser::Reader.new(boundary)
      result = { errors: [], parts: [] }
      def result.part(name)
        hash = self[:parts].detect { |h| h[:part].name == name }
        [hash[:part], hash[:body].join]
      end

      reader.on_part do |part|
        result[:parts] << thispart = {
          part: part,
          body: []
        }
        part.on_data do |chunk|
          thispart[:body] << chunk
        end
      end
      reader.on_error do |msg|
        result[:errors] << msg
      end
      reader.write(body)
      result
    end

    def method_with_body?(method)
      self.class.method_with_body?(method)
    end

    def big_string
      kb = 1024
      (32..126).map(&:chr).cycle.take(50 * kb).join
    end
  end
end
