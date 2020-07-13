# frozen_string_literal: true

require "zlib"
require "time"  # for Time.httpdate

module Rack
  # This middleware enables content encoding of http responses,
  # usually for purposes of compression.
  #
  # Currently supported encodings:
  #
  # * gzip
  # * identity (no transformation)
  #
  # This middleware automatically detects when encoding is supported
  # and allowed. For example no encoding is made when a cache
  # directive of 'no-transform' is present, when the response status
  # code is one that doesn't allow an entity body, or when the body
  # is empty.
  #
  # Note that despite the name, Deflater does not support the +deflate+
  # encoding.
  class Deflater
    (require_relative 'core_ext/regexp'; using ::Rack::RegexpExtensions) if RUBY_VERSION < '2.4'

    # Creates Rack::Deflater middleware. Options:
    #
    # :if :: a lambda enabling / disabling deflation based on returned boolean value
    #        (e.g <tt>use Rack::Deflater, :if => lambda { |*, body| sum=0; body.each { |i| sum += i.length }; sum > 512 }</tt>).
    #        However, be aware that calling `body.each` inside the block will break cases where `body.each` is not idempotent,
    #        such as when it is an +IO+ instance.
    # :include :: a list of content types that should be compressed. By default, all content types are compressed.
    # :sync :: determines if the stream is going to be flushed after every chunk.  Flushing after every chunk reduces
    #          latency for time-sensitive streaming applications, but hurts compression and throughput.
    #          Defaults to +true+.
    def initialize(app, options = {})
      @app = app
      @condition = options[:if]
      @compressible_types = options[:include]
      @sync = options.fetch(:sync, true)
    end

    def call(env)
      status, headers, body = @app.call(env)
      headers = Utils::HeaderHash[headers]

      unless should_deflate?(env, status, headers, body)
        return [status, headers, body]
      end

      request = Request.new(env)

      encoding = Utils.select_best_encoding(%w(gzip identity),
                                            request.accept_encoding)

      # Set the Vary HTTP header.
      vary = headers["Vary"].to_s.split(",").map(&:strip)
      unless vary.include?("*") || vary.include?("Accept-Encoding")
        headers["Vary"] = vary.push("Accept-Encoding").join(",")
      end

      case encoding
      when "gzip"
        headers['Content-Encoding'] = "gzip"
        headers.delete(CONTENT_LENGTH)
        mtime = headers["Last-Modified"]
        mtime = Time.httpdate(mtime).to_i if mtime
        [status, headers, GzipStream.new(body, mtime, @sync)]
      when "identity"
        [status, headers, body]
      when nil
        message = "An acceptable encoding for the requested resource #{request.fullpath} could not be found."
        bp = Rack::BodyProxy.new([message]) { body.close if body.respond_to?(:close) }
        [406, { CONTENT_TYPE => "text/plain", CONTENT_LENGTH => message.length.to_s }, bp]
      end
    end

    # Body class used for gzip encoded responses.
    class GzipStream
      # Initialize the gzip stream.  Arguments:
      # body :: Response body to compress with gzip
      # mtime :: The modification time of the body, used to set the
      #          modification time in the gzip header.
      # sync :: Whether to flush each gzip chunk as soon as it is ready.
      def initialize(body, mtime, sync)
        @body = body
        @mtime = mtime
        @sync = sync
      end

      # Yield gzip compressed strings to the given block.
      def each(&block)
        @writer = block
        gzip = ::Zlib::GzipWriter.new(self)
        gzip.mtime = @mtime if @mtime
        @body.each { |part|
          # Skip empty strings, as they would result in no output,
          # and flushing empty parts would raise Zlib::BufError.
          next if part.empty?

          gzip.write(part)
          gzip.flush if @sync
        }
      ensure
        gzip.close
      end

      # Call the block passed to #each with the the gzipped data.
      def write(data)
        @writer.call(data)
      end

      # Close the original body if possible.
      def close
        @body.close if @body.respond_to?(:close)
      end
    end

    private

    # Whether the body should be compressed.
    def should_deflate?(env, status, headers, body)
      # Skip compressing empty entity body responses and responses with
      # no-transform set.
      if Utils::STATUS_WITH_NO_ENTITY_BODY.key?(status.to_i) ||
          /\bno-transform\b/.match?(headers['Cache-Control'].to_s) ||
          headers['Content-Encoding']&.!~(/\bidentity\b/)
        return false
      end

      # Skip if @compressible_types are given and does not include request's content type
      return false if @compressible_types && !(headers.has_key?('Content-Type') && @compressible_types.include?(headers['Content-Type'][/[^;]*/]))

      # Skip if @condition lambda is given and evaluates to false
      return false if @condition && !@condition.call(env, status, headers, body)

      # No point in compressing empty body, also handles usage with
      # Rack::Sendfile.
      return false if headers[CONTENT_LENGTH] == '0'

      true
    end
  end
end
