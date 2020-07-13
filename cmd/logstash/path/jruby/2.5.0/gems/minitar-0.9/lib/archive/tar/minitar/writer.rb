# coding: utf-8

module Archive::Tar::Minitar
  # The class that writes a tar format archive to a data stream.
  class Writer
    include Archive::Tar::Minitar::ByteSize

    # The exception raised when the user attempts to write more data to a
    # BoundedWriteStream than has been allocated.
    WriteBoundaryOverflow = Class.new(StandardError)

    # A stream wrapper that can only be written to. Any attempt to read
    # from this restricted stream will result in a NameError being thrown.
    class WriteOnlyStream
      def initialize(io)
        @io = io
      end

      def write(data)
        @io.write(data)
      end
    end

    private_constant :WriteOnlyStream if respond_to?(:private_constant)

    # A WriteOnlyStream that also has a size limit.
    class BoundedWriteStream < WriteOnlyStream
      include Archive::Tar::Minitar::ByteSize

      def self.const_missing(c)
        case c
        when :FileOverflow
          warn 'Writer::BoundedWriteStream::FileOverflow has been renamed ' \
            'to Writer::WriteBoundaryOverflow'
          const_set :FileOverflow,
            Archive::Tar::Minitar::Writer::WriteBoundaryOverflow
        else
          super
        end
      end

      # The maximum number of bytes that may be written to this data
      # stream.
      attr_reader :limit
      # The current total number of bytes written to this data stream.
      attr_reader :written

      def initialize(io, limit)
        @io       = io
        @limit    = limit
        @written  = 0
      end

      def write(data)
        size = bytesize(data)
        raise WriteBoundaryOverflow if (size + @written) > @limit
        @io.write(data)
        @written += size
        size
      end
    end

    private_constant :BoundedWriteStream if respond_to?(:private_constant)

    def self.const_missing(c)
      case c
      when :BoundedStream
        warn 'BoundedStream has been renamed to BoundedWriteStream'
        const_set(:BoundedStream, BoundedWriteStream)
      else
        super
      end
    end

    # With no associated block, +Writer::open+ is a synonym for +Writer::new+.
    # If the optional code block is given, it will be passed the new _writer_
    # as an argument and the Writer object will automatically be closed when
    # the block terminates. In this instance, +Writer::open+ returns the value
    # of the block.
    #
    # call-seq:
    #    w = Archive::Tar::Minitar::Writer.open(STDOUT)
    #    w.add_file_simple('foo.txt', :size => 3)
    #    w.close
    #
    #    Archive::Tar::Minitar::Writer.open(STDOUT) do |w|
    #      w.add_file_simple('foo.txt', :size => 3)
    #    end
    def self.open(io) # :yields Writer:
      writer = new(io)
      return writer unless block_given?

      # This exception context must remain, otherwise the stream closes on open
      # even if a block is not given.
      begin
        yield writer
      ensure
        writer.close
      end
    end

    # Creates and returns a new Writer object.
    def initialize(io)
      @io     = io
      @closed = false
    end

    # Adds a file to the archive as +name+. The data can be provided in the
    # <tt>opts[:data]</tt> or provided to a BoundedWriteStream that is
    # yielded to the provided block.
    #
    # If <tt>opts[:data]</tt> is provided, all other values to +opts+ are
    # optional. If the data is provided to the yielded BoundedWriteStream,
    # <tt>opts[:size]</tt> must be provided.
    #
    # Valid parameters to +opts+ are:
    #
    # <tt>:data</tt>::  Optional. The data to write to the archive.
    # <tt>:mode</tt>::  The Unix file permissions mode value. If not
    #                   provided, defaults to 0644.
    # <tt>:size</tt>::  The size, in bytes. If <tt>:data</tt> is provided,
    #                   this parameter may be ignored (if it is less than
    #                   the size of the data provided) or used to add
    #                   padding (if it is greater than the size of the data
    #                   provided).
    # <tt>:uid</tt>::   The Unix file owner user ID number.
    # <tt>:gid</tt>::   The Unix file owner group ID number.
    # <tt>:mtime</tt>:: File modification time, interpreted as an integer.
    #
    # An exception will be raised if the Writer is already closed, or if
    # more data is written to the BoundedWriteStream than expected.
    #
    # call-seq:
    #    writer.add_file_simple('foo.txt', :data => "bar")
    #    writer.add_file_simple('foo.txt', :size => 3) do |w|
    #      w.write("bar")
    #    end
    def add_file_simple(name, opts = {}) # :yields BoundedWriteStream:
      raise ClosedStream if @closed

      header = {
        :mode   => opts.fetch(:mode, 0o644),
        :mtime  => opts.fetch(:mtime, nil),
        :gid    => opts.fetch(:gid, nil),
        :uid    => opts.fetch(:uid, nil)
      }

      data = opts.fetch(:data, nil)
      size = opts.fetch(:size, nil)

      if block_given?
        if data
          raise ArgumentError,
            'Too much data (opts[:data] and block_given?).'
        end

        raise ArgumentError, 'No size provided' unless size
      else
        raise ArgumentError, 'No data provided' unless data

        bytes = bytesize(data)
        size = bytes if size.nil? || size < bytes
      end

      header[:size] = size

      write_header(name, header)

      os = BoundedWriteStream.new(@io, size)
      if block_given?
        yield os
      else
        os.write(data)
      end

      min_padding = size - os.written
      @io.write("\0" * min_padding)
      remainder = (512 - (size % 512)) % 512
      @io.write("\0" * remainder)
    end

    # Adds a file to the archive as +name+. The data can be provided in the
    # <tt>opts[:data]</tt> or provided to a yielded +WriteOnlyStream+. The
    # size of the file will be determined from the amount of data written
    # to the stream.
    #
    # Valid parameters to +opts+ are:
    #
    # <tt>:mode</tt>::  The Unix file permissions mode value. If not
    #                   provided, defaults to 0644.
    # <tt>:uid</tt>::   The Unix file owner user ID number.
    # <tt>:gid</tt>::   The Unix file owner group ID number.
    # <tt>:mtime</tt>:: File modification time, interpreted as an integer.
    # <tt>:data</tt>::  Optional. The data to write to the archive.
    #
    # If <tt>opts[:data]</tt> is provided, this acts the same as
    # #add_file_simple. Otherwise, the file's size will be determined from
    # the amount of data written to the stream.
    #
    # For #add_file to be used without <tt>opts[:data]</tt>, the Writer
    # must be wrapping a stream object that is seekable. Otherwise,
    # #add_file_simple must be used.
    #
    # +opts+ may be modified during the writing of the file to the stream.
    def add_file(name, opts = {}, &block) # :yields WriteOnlyStream, +opts+:
      raise ClosedStream if @closed

      return add_file_simple(name, opts, &block) if opts[:data]

      unless Archive::Tar::Minitar.seekable?(@io)
        raise Archive::Tar::Minitar::NonSeekableStream
      end

      init_pos = @io.pos
      @io.write("\0" * 512) # placeholder for the header

      yield WriteOnlyStream.new(@io), opts

      size      = @io.pos - (init_pos + 512)
      remainder = (512 - (size % 512)) % 512
      @io.write("\0" * remainder)

      final_pos, @io.pos = @io.pos, init_pos

      header = {
        :mode => opts[:mode],
        :mtime => opts[:mtime],
        :size => size,
        :gid => opts[:gid],
        :uid => opts[:uid],
      }
      write_header(name, header)
      @io.pos = final_pos
    end

    # Creates a directory entry in the tar.
    def mkdir(name, opts = {})
      raise ClosedStream if @closed

      header = {
        :mode => opts[:mode],
        :typeflag => '5',
        :size => 0,
        :gid => opts[:gid],
        :uid => opts[:uid],
        :mtime => opts[:mtime],
      }
      write_header(name, header)
      nil
    end

    # Passes the #flush method to the wrapped stream, used for buffered
    # streams.
    def flush
      raise ClosedStream if @closed
      @io.flush if @io.respond_to?(:flush)
    end

    # Returns false if the writer is open.
    def closed?
      @closed
    end

    # Closes the Writer. This does not close the underlying wrapped output
    # stream.
    def close
      return if @closed
      @io.write("\0" * 1024)
      @closed = true
    end

    private

    def write_header(long_name, header)
      short_name, prefix, needs_long_name = split_name(long_name)

      if needs_long_name
        long_name_header = {
          :prefix => '',
          :name => PosixHeader::GNU_EXT_LONG_LINK,
          :typeflag => 'L',
          :size => long_name.length,
          :mode => 0,
        }
        @io.write(PosixHeader.new(long_name_header))
        @io.write(long_name)
        @io.write("\0" * (512 - (long_name.length % 512)))
      end

      new_header = header.merge({ :name => short_name, :prefix => prefix })
      @io.write(PosixHeader.new(new_header))
    end

    def split_name(name)
      if bytesize(name) <= 100
        prefix = ''
      else
        parts = name.split(/\//)
        newname = parts.pop

        nxt = ''

        loop do
          nxt = parts.pop || ''
          break if bytesize(newname) + 1 + bytesize(nxt) >= 100
          newname = "#{nxt}/#{newname}"
        end

        prefix = (parts + [nxt]).join('/')

        name = newname
      end

      [ name, prefix, (bytesize(name) > 100 || bytesize(prefix) > 155) ]
    end
  end
end
