# frozen_string_literal: true

require 'strscan'

module Rack
  module Multipart
    class MultipartPartLimitError < Errno::EMFILE; end

    class Parser
      (require_relative '../core_ext/regexp'; using ::Rack::RegexpExtensions) if RUBY_VERSION < '2.4'

      BUFSIZE = 1_048_576
      TEXT_PLAIN = "text/plain"
      TEMPFILE_FACTORY = lambda { |filename, content_type|
        Tempfile.new(["RackMultipart", ::File.extname(filename.gsub("\0", '%00'))])
      }

      BOUNDARY_REGEX = /\A([^\n]*(?:\n|\Z))/

      class BoundedIO # :nodoc:
        def initialize(io, content_length)
          @io             = io
          @content_length = content_length
          @cursor = 0
        end

        def read(size, outbuf = nil)
          return if @cursor >= @content_length

          left = @content_length - @cursor

          str = if left < size
                  @io.read left, outbuf
                else
                  @io.read size, outbuf
                end

          if str
            @cursor += str.bytesize
          else
            # Raise an error for mismatching Content-Length and actual contents
            raise EOFError, "bad content body"
          end

          str
        end

        def rewind
          @io.rewind
        end
      end

      MultipartInfo = Struct.new :params, :tmp_files
      EMPTY         = MultipartInfo.new(nil, [])

      def self.parse_boundary(content_type)
        return unless content_type
        data = content_type.match(MULTIPART)
        return unless data
        data[1]
      end

      def self.parse(io, content_length, content_type, tmpfile, bufsize, qp)
        return EMPTY if 0 == content_length

        boundary = parse_boundary content_type
        return EMPTY unless boundary

        io = BoundedIO.new(io, content_length) if content_length
        outbuf = String.new

        parser = new(boundary, tmpfile, bufsize, qp)
        parser.on_read io.read(bufsize, outbuf)

        loop do
          break if parser.state == :DONE
          parser.on_read io.read(bufsize, outbuf)
        end

        io.rewind
        parser.result
      end

      class Collector
        class MimePart < Struct.new(:body, :head, :filename, :content_type, :name)
          def get_data
            data = body
            if filename == ""
              # filename is blank which means no file has been selected
              return
            elsif filename
              body.rewind if body.respond_to?(:rewind)

              # Take the basename of the upload's original filename.
              # This handles the full Windows paths given by Internet Explorer
              # (and perhaps other broken user agents) without affecting
              # those which give the lone filename.
              fn = filename.split(/[\/\\]/).last

              data = { filename: fn, type: content_type,
                      name: name, tempfile: body, head: head }
            end

            yield data
          end
        end

        class BufferPart < MimePart
          def file?; false; end
          def close; end
        end

        class TempfilePart < MimePart
          def file?; true; end
          def close; body.close; end
        end

        include Enumerable

        def initialize(tempfile)
          @tempfile = tempfile
          @mime_parts = []
          @open_files = 0
        end

        def each
          @mime_parts.each { |part| yield part }
        end

        def on_mime_head(mime_index, head, filename, content_type, name)
          if filename
            body = @tempfile.call(filename, content_type)
            body.binmode if body.respond_to?(:binmode)
            klass = TempfilePart
            @open_files += 1
          else
            body = String.new
            klass = BufferPart
          end

          @mime_parts[mime_index] = klass.new(body, head, filename, content_type, name)

          check_open_files
        end

        def on_mime_body(mime_index, content)
          @mime_parts[mime_index].body << content
        end

        def on_mime_finish(mime_index)
        end

        private

        def check_open_files
          if Utils.multipart_part_limit > 0
            if @open_files >= Utils.multipart_part_limit
              @mime_parts.each(&:close)
              raise MultipartPartLimitError, 'Maximum file multiparts in content reached'
            end
          end
        end
      end

      attr_reader :state

      def initialize(boundary, tempfile, bufsize, query_parser)
        @query_parser   = query_parser
        @params         = query_parser.make_params
        @boundary       = "--#{boundary}"
        @bufsize        = bufsize

        @full_boundary = @boundary
        @end_boundary = @boundary + '--'
        @state = :FAST_FORWARD
        @mime_index = 0
        @collector = Collector.new tempfile

        @sbuf = StringScanner.new("".dup)
        @body_regex = /(?:#{EOL})?#{Regexp.quote(@boundary)}(?:#{EOL}|--)/m
        @rx_max_size = EOL.size + @boundary.bytesize + [EOL.size, '--'.size].max
        @head_regex = /(.*?#{EOL})#{EOL}/m
      end

      def on_read(content)
        handle_empty_content!(content)
        @sbuf.concat content
        run_parser
      end

      def result
        @collector.each do |part|
          part.get_data do |data|
            tag_multipart_encoding(part.filename, part.content_type, part.name, data)
            @query_parser.normalize_params(@params, part.name, data, @query_parser.param_depth_limit)
          end
        end
        MultipartInfo.new @params.to_params_hash, @collector.find_all(&:file?).map(&:body)
      end

      private

      def run_parser
        loop do
          case @state
          when :FAST_FORWARD
            break if handle_fast_forward == :want_read
          when :CONSUME_TOKEN
            break if handle_consume_token == :want_read
          when :MIME_HEAD
            break if handle_mime_head == :want_read
          when :MIME_BODY
            break if handle_mime_body == :want_read
          when :DONE
            break
          end
        end
      end

      def handle_fast_forward
        if consume_boundary
          @state = :MIME_HEAD
        else
          raise EOFError, "bad content body" if @sbuf.rest_size >= @bufsize
          :want_read
        end
      end

      def handle_consume_token
        tok = consume_boundary
        # break if we're at the end of a buffer, but not if it is the end of a field
        @state = if tok == :END_BOUNDARY || (@sbuf.eos? && tok != :BOUNDARY)
          :DONE
        else
          :MIME_HEAD
        end
      end

      def handle_mime_head
        if @sbuf.scan_until(@head_regex)
          head = @sbuf[1]
          content_type = head[MULTIPART_CONTENT_TYPE, 1]
          if name = head[MULTIPART_CONTENT_DISPOSITION, 1]
            name = Rack::Auth::Digest::Params::dequote(name)
          else
            name = head[MULTIPART_CONTENT_ID, 1]
          end

          filename = get_filename(head)

          if name.nil? || name.empty?
            name = filename || "#{content_type || TEXT_PLAIN}[]".dup
          end

          @collector.on_mime_head @mime_index, head, filename, content_type, name
          @state = :MIME_BODY
        else
          :want_read
        end
      end

      def handle_mime_body
        if (body_with_boundary = @sbuf.check_until(@body_regex)) # check but do not advance the pointer yet
          body = body_with_boundary.sub(/#{@body_regex}\z/m, '') # remove the boundary from the string
          @collector.on_mime_body @mime_index, body
          @sbuf.pos += body.length + 2 # skip \r\n after the content
          @state = :CONSUME_TOKEN
          @mime_index += 1
        else
          # Save what we have so far
          if @rx_max_size < @sbuf.rest_size
            delta = @sbuf.rest_size - @rx_max_size
            @collector.on_mime_body @mime_index, @sbuf.peek(delta)
            @sbuf.pos += delta
            @sbuf.string = @sbuf.rest
          end
          :want_read
        end
      end

      def full_boundary; @full_boundary; end

      def consume_boundary
        while read_buffer = @sbuf.scan_until(BOUNDARY_REGEX)
          case read_buffer.strip
          when full_boundary then return :BOUNDARY
          when @end_boundary then return :END_BOUNDARY
          end
          return if @sbuf.eos?
        end
      end

      def get_filename(head)
        filename = nil
        case head
        when RFC2183
          params = Hash[*head.scan(DISPPARM).flat_map(&:compact)]

          if filename = params['filename']
            filename = $1 if filename =~ /^"(.*)"$/
          elsif filename = params['filename*']
            encoding, _, filename = filename.split("'", 3)
          end
        when BROKEN_QUOTED, BROKEN_UNQUOTED
          filename = $1
        end

        return unless filename

        if filename.scan(/%.?.?/).all? { |s| /%[0-9a-fA-F]{2}/.match?(s) }
          filename = Utils.unescape_path(filename)
        end

        filename.scrub!

        if filename !~ /\\[^\\"]/
          filename = filename.gsub(/\\(.)/, '\1')
        end

        if encoding
          filename.force_encoding ::Encoding.find(encoding)
        end

        filename
      end

      CHARSET = "charset"

      def tag_multipart_encoding(filename, content_type, name, body)
        name = name.to_s
        encoding = Encoding::UTF_8

        name.force_encoding(encoding)

        return if filename

        if content_type
          list         = content_type.split(';')
          type_subtype = list.first
          type_subtype.strip!
          if TEXT_PLAIN == type_subtype
            rest = list.drop 1
            rest.each do |param|
              k, v = param.split('=', 2)
              k.strip!
              v.strip!
              v = v[1..-2] if v.start_with?('"') && v.end_with?('"')
              encoding = Encoding.find v if k == CHARSET
            end
          end
        end

        name.force_encoding(encoding)
        body.force_encoding(encoding)
      end

      def handle_empty_content!(content)
        if content.nil? || content.empty?
          raise EOFError
        end
      end
    end
  end
end
