# coding: utf-8

require 'archive/tar/minitar/writer'

module Archive::Tar::Minitar
  # Wraps a Archive::Tar::Minitar::Writer with convenience methods and wrapped
  # stream management. If the stream provided to Output does not support random
  # access, only Writer#add_file_simple and Writer#mkdir are guaranteed to
  # work.
  class Output
    # With no associated block, +Output.open+ is a synonym for +Output.new+. If
    # the optional code block is given, it will be given the new Output as an
    # argument and the Output object will automatically be closed when the
    # block terminates (this also closes the wrapped stream object). In this
    # instance, +Output.open+ returns the value of the block.
    #
    # call-seq:
    #    Archive::Tar::Minitar::Output.open(io) -> output
    #    Archive::Tar::Minitar::Output.open(io) { |output| block } -> obj
    def self.open(output)
      stream = new(output)
      return stream unless block_given?

      # This exception context must remain, otherwise the stream closes on open
      # even if a block is not given.
      begin
        yield stream
      ensure
        stream.close
      end
    end

    # Output.tar is a wrapper for Output.open that yields the owned tar object
    # instead of the Output object. If a block is not provided, an enumerator
    # will be created with the same behaviour.
    #
    # call-seq:
    #    Archive::Tar::Minitar::Output.tar(io) -> enumerator
    #    Archive::Tar::Minitar::Output.tar(io) { |tar| block } -> obj
    def self.tar(output)
      return to_enum(__method__, output) unless block_given?

      open(output) do |stream|
        yield stream.tar
      end
    end

    # Creates a new Output object. If +output+ is a stream object that responds
    # to #write, then it will simply be wrapped. Otherwise, one will be created
    # and opened using Kernel#open. When Output#close is called, the stream
    # object wrapped will be closed.
    #
    # call-seq:
    #    Archive::Tar::Minitar::Output.new(io) -> output
    #    Archive::Tar::Minitar::Output.new(path) -> output
    def initialize(output)
      @io = if output.respond_to?(:write)
              output
            else
              ::Kernel.open(output, 'wb')
            end
      @tar = Archive::Tar::Minitar::Writer.new(@io)
    end

    # Returns the Writer object for direct access.
    attr_reader :tar

    # Returns false if the wrapped data stream is open.
    def closed?
      @io.closed?
    end

    # Closes the Writer object and the wrapped data stream.
    def close
      @tar.close
      @io.close
    end
  end
end
