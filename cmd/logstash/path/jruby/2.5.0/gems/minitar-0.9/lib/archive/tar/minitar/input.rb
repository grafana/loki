# coding: utf-8

require 'archive/tar/minitar/reader'

module Archive::Tar::Minitar
  # Wraps a Archive::Tar::Minitar::Reader with convenience methods and wrapped
  # stream management; Input only works with data streams that can be rewound.
  class Input
    include Enumerable

    # With no associated block, +Input.open+ is a synonym for +Input.new+. If
    # the optional code block is given, it will be given the new Input as an
    # argument and the Input object will automatically be closed when the block
    # terminates (this also closes the wrapped stream object). In this
    # instance, +Input.open+ returns the value of the block.
    #
    # call-seq:
    #    Archive::Tar::Minitar::Input.open(io) -> input
    #    Archive::Tar::Minitar::Input.open(io) { |input| block } -> obj
    def self.open(input)
      stream = new(input)

      if block_given?
        # This exception context must remain, otherwise the stream closes on
        # open even if a block is not given.
        begin
          yield stream
        ensure
          stream.close
        end
      else
        stream
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
    #    Archive::Tar::Minitar::Input.each_entry(io) -> enumerator
    #    Archive::Tar::Minitar::Input.each_entry(io) { |entry| block } -> obj
    def self.each_entry(input)
      return to_enum(__method__, input) unless block_given?

      open(input) do |stream|
        stream.each do |entry|
          yield entry
        end
      end
    end

    # Creates a new Input object. If +input+ is a stream object that responds
    # to #read, then it will simply be wrapped. Otherwise, one will be created
    # and opened using Kernel#open. When Input#close is called, the stream
    # object wrapped will be closed.
    #
    # An exception will be raised if the stream that is wrapped does not
    # support rewinding.
    #
    # call-seq:
    #    Archive::Tar::Minitar::Input.new(io) -> input
    #    Archive::Tar::Minitar::Input.new(path) -> input
    def initialize(input)
      @io = if input.respond_to?(:read)
              input
            else
              ::Kernel.open(input, 'rb')
            end

      unless Archive::Tar::Minitar.seekable?(@io, :rewind)
        raise Archive::Tar::Minitar::NonSeekableStream
      end

      @tar = Reader.new(@io)
    end

    # When provided a block, iterates through each entry in the archive. When
    # finished, rewinds to the beginning of the stream.
    #
    # If not provided a block, creates an enumerator with the same semantics.
    def each_entry
      return to_enum unless block_given?

      @tar.each do |entry|
        yield entry
      end
    ensure
      @tar.rewind
    end
    alias each each_entry

    # Extracts the current +entry+ to +destdir+. If a block is provided, it
    # yields an +action+ Symbol, the full name of the file being extracted
    # (+name+), and a Hash of statistical information (+stats+).
    #
    # The +action+ will be one of:
    # <tt>:dir</tt>::           The +entry+ is a directory.
    # <tt>:file_start</tt>::    The +entry+ is a file; the extract of the
    #                           file is just beginning.
    # <tt>:file_progress</tt>:: Yielded every 4096 bytes during the extract
    #                           of the +entry+.
    # <tt>:file_done</tt>::     Yielded when the +entry+ is completed.
    #
    # The +stats+ hash contains the following keys:
    # <tt>:current</tt>:: The current total number of bytes read in the
    #                     +entry+.
    # <tt>:currinc</tt>:: The current number of bytes read in this read
    #                     cycle.
    # <tt>:entry</tt>::   The entry being extracted; this is a
    #                     Reader::EntryStream, with all methods thereof.
    def extract_entry(destdir, entry, options = {}, &block) # :yields action, name, stats:
      stats = {
        :current  => 0,
        :currinc  => 0,
        :entry    => entry
      }

      # extract_entry is not vulnerable to prefix '/' vulnerabilities, but it
      # is vulnerable to relative path directories. This code will break this
      # vulnerability. For this version, we are breaking relative paths HARD by
      # throwing an exception.
      #
      # Future versions may permit relative paths as long as the file does not
      # leave +destdir+.
      #
      # However, squeeze consecutive '/' characters together.
      full_name = entry.full_name.squeeze('/')

      if full_name =~ /\.{2}(?:\/|\z)/
        raise SecureRelativePathError, %q(Path contains '..')
      end

      if entry.directory?
        extract_directory(destdir, full_name, entry, stats, options, &block)
      else # it's a file
        extract_file(destdir, full_name, entry, stats, options, &block)
      end
    end

    # Returns false if the wrapped data stream is open.
    def closed?
      @io.closed?
    end

    # Returns the Reader object for direct access.
    attr_reader :tar

    # Closes both the Reader object and the wrapped data stream.
    def close
      @io.close
      @tar.close
    end

    private

    def fsync_dir(dirname)
      # make sure this hits the disc
      dir = open(dirname, 'rb')
      dir.fsync
    rescue # ignore IOError if it's an unpatched (old) Ruby
      nil
    ensure
      dir.close if dir rescue nil # rubocop:disable Style/RescueModifier
    end

    def extract_directory(destdir, full_name, entry, stats, options)
      dest = File.join(destdir, full_name)

      yield :dir, full_name, stats if block_given?

      if Archive::Tar::Minitar.dir?(dest)
        begin
          FileUtils.chmod(entry.mode, dest)
        rescue
          nil
        end
      else
        File.unlink(dest.chomp('/')) if File.symlink?(dest.chomp('/'))

        FileUtils.mkdir_p(dest, :mode => entry.mode)
        FileUtils.chmod(entry.mode, dest)
      end

      if options.fetch(:fsync, true)
        fsync_dir(dest)
        fsync_dir(File.join(dest, '..'))
      end
    end

    def extract_file(destdir, full_name, entry, stats, options)
      destdir = File.join(destdir, File.dirname(full_name))
      FileUtils.mkdir_p(destdir, :mode => 0o755)

      destfile = File.join(destdir, File.basename(full_name))

      File.unlink(destfile) if File.symlink?(destfile)

      # Errno::ENOENT
      # rubocop:disable Style/RescueModifier
      FileUtils.chmod(0o600, destfile) rescue nil
      # rubocop:enable Style/RescueModifier

      yield :file_start, full_name, stats if block_given?

      File.open(destfile, 'wb', entry.mode) do |os|
        loop do
          data = entry.read(4096)
          break unless data

          stats[:currinc] = os.write(data)
          stats[:current] += stats[:currinc]

          yield :file_progress, full_name, stats if block_given?
        end

        if options.fetch(:fsync, true)
          yield :file_fsync, full_name, stats if block_given?
          os.fsync
        end
      end

      FileUtils.chmod(entry.mode, destfile)

      if options.fetch(:fsync, true)
        yield :dir_fsync, full_name, stats if block_given?

        fsync_dir(File.dirname(destfile))
        fsync_dir(File.join(File.dirname(destfile), '..'))
      end

      yield :file_done, full_name, stats if block_given?
    end
  end
end
