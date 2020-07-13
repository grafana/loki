# coding: utf-8

module Archive::Tar::Minitar
  # The class that reads a tar format archive from a data stream. The data
  # stream may be sequential or random access, but certain features only work
  # with random access data streams.
  class Reader
    include Enumerable
    include Archive::Tar::Minitar::ByteSize

    # This marks the EntryStream closed for reading without closing the
    # actual data stream.
    module InvalidEntryStream
      # rubocop:disable Style/SingleLineMethods
      # rubocop:disable Style/EmptyLineBetweenDefs
      def read(*); raise ClosedStream; end
      def getc; raise ClosedStream; end
      def rewind; raise ClosedStream; end
      def closed?; true; end
      # rubocop:enable Style/EmptyLineBetweenDefs
      # rubocop:enable Style/SingleLineMethods Style/EmptyLineBetweenDefs
    end

    # EntryStreams are pseudo-streams on top of the main data stream.
    class EntryStream
      include Archive::Tar::Minitar::ByteSize

      Archive::Tar::Minitar::PosixHeader::FIELDS.each do |field|
        attr_reader field.to_sym
      end

      def initialize(header, io)
        @io       = io
        @name     = header.name
        @mode     = header.mode
        @uid      = header.uid
        @gid      = header.gid
        @size     = header.size
        @mtime    = header.mtime
        @checksum = header.checksum
        @typeflag = header.typeflag
        @linkname = header.linkname
        @magic    = header.magic
        @version  = header.version
        @uname    = header.uname
        @gname    = header.gname
        @devmajor = header.devmajor
        @devminor = header.devminor
        @prefix   = header.prefix
        @read     = 0
        @orig_pos =
          if Archive::Tar::Minitar.seekable?(@io)
            @io.pos
          else
            0
          end
      end

      # Reads +len+ bytes (or all remaining data) from the entry. Returns
      # +nil+ if there is no more data to read.
      def read(len = nil)
        return nil if @read >= @size
        len ||= @size - @read
        max_read = [len, @size - @read].min
        ret = @io.read(max_read)
        @read += bytesize(ret)
        ret
      end

      # Reads one byte from the entry. Returns +nil+ if there is no more data
      # to read.
      def getc
        return nil if @read >= @size
        ret = @io.getc
        @read += 1 if ret
        ret
      end

      # Returns +true+ if the entry represents a directory.
      def directory?
        case @typeflag
        when '5'
          true
        when '0', "\0"
          # If the name ends with a slash, treat it as a directory.
          # This is what other major tar implementations do for
          # interoperability and compatibility with older tar variants
          # and some new ones.
          @name.end_with?('/')
        else
          false
        end
      end
      alias directory directory?

      # Returns +true+ if the entry represents a plain file.
      def file?
        (@typeflag == '0' || @typeflag == "\0") && !@name.end_with?('/')
      end
      alias file file?

      # Returns +true+ if the current read pointer is at the end of the
      # EntryStream data.
      def eof?
        @read >= @size
      end

      # Returns the current read pointer in the EntryStream.
      def pos
        @read
      end

      # Sets the current read pointer to the beginning of the EntryStream.
      def rewind
        unless Archive::Tar::Minitar.seekable?(@io, :pos=)
          raise Archive::Tar::Minitar::NonSeekableStream
        end
        @io.pos = @orig_pos
        @read = 0
      end

      def bytes_read
        @read
      end

      # Returns the full and proper name of the entry.
      def full_name
        if @prefix != ''
          File.join(@prefix, @name)
        else
          @name
        end
      end

      # Returns false if the entry stream is valid.
      def closed?
        false
      end

      # Closes the entry.
      def close
        invalidate
      end

      private

      def invalidate
        extend InvalidEntryStream
      end
    end

    # With no associated block, +Reader::open+ is a synonym for
    # +Reader::new+. If the optional code block is given, it will be passed
    # the new _writer_ as an argument and the Reader object will
    # automatically be closed when the block terminates. In this instance,
    # +Reader::open+ returns the value of the block.
    def self.open(io)
      reader = new(io)
      return reader unless block_given?

      # This exception context must remain, otherwise the stream closes on open
      # even if a block is not given.
      begin
        yield reader
      ensure
        reader.close
      end
    end

    # Iterates over each entry in the provided input. This wraps the common
    # pattern of:
    #
    #     Archive::Tar::Minitar::Input.open(io) do |i|
    #       inp.each do |entry|
    #         # ...
    #       end
    #     end
    #
    # If a block is not provided, an enumerator will be created with the same
    # behaviour.
    #
    # call-seq:
    #    Archive::Tar::Minitar::Reader.each_entry(io) -> enumerator
    #    Archive::Tar::Minitar::Reader.each_entry(io) { |entry| block } -> obj
    def self.each_entry(io)
      return to_enum(__method__, io) unless block_given?

      open(io) do |reader|
        reader.each_entry do |entry|
          yield entry
        end
      end
    end

    # Creates and returns a new Reader object.
    def initialize(io)
      @io = io
      @init_pos = io.pos rescue nil
    end

    # Resets the read pointer to the beginning of data stream. Do not call
    # this during a #each or #each_entry iteration. This only works with
    # random access data streams that respond to #rewind and #pos.
    def rewind
      if @init_pos.zero?
        unless Archive::Tar::Minitar.seekable?(@io, :rewind)
          raise Archive::Tar::Minitar::NonSeekableStream
        end
        @io.rewind
      else
        unless Archive::Tar::Minitar.seekable?(@io, :pos=)
          raise Archive::Tar::Minitar::NonSeekableStream
        end
        @io.pos = @init_pos
      end
    end

    # Iterates through each entry in the data stream.
    def each_entry
      return to_enum unless block_given?

      loop do
        return if @io.eof?

        header = Archive::Tar::Minitar::PosixHeader.from_stream(@io)
        raise Archive::Tar::Minitar::InvalidTarStream unless header.valid?
        return if header.empty?

        raise Archive::Tar::Minitar::InvalidTarStream if header.size < 0

        if header.long_name?
          name = @io.read(512).rstrip
          header = PosixHeader.from_stream(@io)
          return if header.empty?
          header.name = name
        end

        entry = EntryStream.new(header, @io)
        size  = entry.size

        yield entry

        skip = (512 - (size % 512)) % 512

        if Archive::Tar::Minitar.seekable?(@io, :seek)
          # avoid reading...
          @io.seek(size - entry.bytes_read, IO::SEEK_CUR)
        else
          pending = size - entry.bytes_read
          while pending > 0
            bread = bytesize(@io.read([pending, 4096].min))
            raise UnexpectedEOF if @io.eof?
            pending -= bread
          end
        end

        @io.read(skip) # discard trailing zeros
        # make sure nobody can use #read, #getc or #rewind anymore
        entry.close
      end
    end
    alias each each_entry

    # Returns false if the reader is open (it never closes).
    def closed?
      false
    end

    def close
    end
  end
end
