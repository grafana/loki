# frozen_string_literal: true

module Rack
  module Multipart
    class UploadedFile
      # The filename, *not* including the path, of the "uploaded" file
      attr_reader :original_filename

      # The content type of the "uploaded" file
      attr_accessor :content_type

      def initialize(filepath = nil, ct = "text/plain", bin = false,
                     path: filepath, content_type: ct, binary: bin, filename: nil, io: nil)
        if io
          @tempfile = io
          @original_filename = filename
        else
          raise "#{path} file does not exist" unless ::File.exist?(path)
          @original_filename = filename || ::File.basename(path)
          @tempfile = Tempfile.new([@original_filename, ::File.extname(path)], encoding: Encoding::BINARY)
          @tempfile.binmode if binary
          FileUtils.copy_file(path, @tempfile.path)
        end
        @content_type = content_type
      end

      def path
        @tempfile.path if @tempfile.respond_to?(:path)
      end
      alias_method :local_path, :path

      def respond_to?(*args)
        super or @tempfile.respond_to?(*args)
      end

      def method_missing(method_name, *args, &block) #:nodoc:
        @tempfile.__send__(method_name, *args, &block)
      end
    end
  end
end
