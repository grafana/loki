# frozen_string_literal: true

module Rack
  module Multipart
    class Generator
      def initialize(params, first = true)
        @params, @first = params, first

        if @first && !@params.is_a?(Hash)
          raise ArgumentError, "value must be a Hash"
        end
      end

      def dump
        return nil if @first && !multipart?
        return flattened_params unless @first

        flattened_params.map do |name, file|
          if file.respond_to?(:original_filename)
            if file.path
              ::File.open(file.path, 'rb') do |f|
                f.set_encoding(Encoding::BINARY)
                content_for_tempfile(f, file, name)
              end
            else
              content_for_tempfile(file, file, name)
            end
          else
            content_for_other(file, name)
          end
        end.join << "--#{MULTIPART_BOUNDARY}--\r"
      end

      private
      def multipart?
        query = lambda { |value|
          case value
          when Array
            value.any?(&query)
          when Hash
            value.values.any?(&query)
          when Rack::Multipart::UploadedFile
            true
          end
        }

        @params.values.any?(&query)
      end

      def flattened_params
        @flattened_params ||= begin
          h = Hash.new
          @params.each do |key, value|
            k = @first ? key.to_s : "[#{key}]"

            case value
            when Array
              value.map { |v|
                Multipart.build_multipart(v, false).each { |subkey, subvalue|
                  h["#{k}[]#{subkey}"] = subvalue
                }
              }
            when Hash
              Multipart.build_multipart(value, false).each { |subkey, subvalue|
                h[k + subkey] = subvalue
              }
            else
              h[k] = value
            end
          end
          h
        end
      end

      def content_for_tempfile(io, file, name)
        length = ::File.stat(file.path).size if file.path
        filename = "; filename=\"#{Utils.escape(file.original_filename)}\"" if file.original_filename
<<-EOF
--#{MULTIPART_BOUNDARY}\r
Content-Disposition: form-data; name="#{name}"#{filename}\r
Content-Type: #{file.content_type}\r
#{"Content-Length: #{length}\r\n" if length}\r
#{io.read}\r
EOF
      end

      def content_for_other(file, name)
<<-EOF
--#{MULTIPART_BOUNDARY}\r
Content-Disposition: form-data; name="#{name}"\r
\r
#{file}\r
EOF
      end
    end
  end
end
