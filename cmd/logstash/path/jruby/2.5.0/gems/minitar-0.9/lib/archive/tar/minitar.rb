# coding: utf-8

##
module Archive; end
##
module Archive::Tar; end

require 'fileutils'
require 'rbconfig'

class << Archive::Tar #:nodoc:
  def const_missing(const) #:nodoc:
    case const
    when :PosixHeader
      warn 'Archive::Tar::PosixHeader has been renamed to ' \
        'Archive::Tar::Minitar::PosixHeader'
      const_set :PosixHeader, Archive::Tar::Minitar::PosixHeader
    else
      super
    end
  end

  private

  def included(mod)
    return if modules.include?(mod)
    warn "Including Minitar via the #{self} namespace is deprecated."
    modules.add mod
  end

  def modules
    require 'set'
    @modules ||= Set.new
  end
end

# == Synopsis
#
# Using minitar is easy. The simplest case is:
#
#     require 'zlib'
#     require 'minitar'
#
#     # Packs everything that matches Find.find('tests').
#     # test.tar will automatically be closed by Minitar.pack.
#     Minitar.pack('tests', File.open('test.tar', 'wb'))
#
#     # Unpacks 'test.tar' to 'x', creating 'x' if necessary.
#     Minitar.unpack('test.tar', 'x')
#
# A gzipped tar can be written with:
#
#     # test.tgz will be closed automatically.
#     Minitar.pack('tests', Zlib::GzipWriter.new(File.open('test.tgz', 'wb'))
#
#     # test.tgz will be closed automatically.
#     Minitar.unpack(Zlib::GzipReader.new(File.open('test.tgz', 'rb')), 'x')
#
# As the case above shows, one need not write to a file. However, it will
# sometimes require that one dive a little deeper into the API, as in the case
# of StringIO objects. Note that I'm not providing a block with
# Minitar::Output, as Minitar::Output#close automatically closes both the
# Output object and the wrapped data stream object.
#
#     begin
#       sgz = Zlib::GzipWriter.new(StringIO.new(""))
#       tar = Output.new(sgz)
#       Find.find('tests') do |entry|
#         Minitar.pack_file(entry, tar)
#       end
#     ensure
#         # Closes both tar and sgz.
#       tar.close
#     end
module Archive::Tar::Minitar
  VERSION = '0.9'.freeze # :nodoc:

  # The base class for any minitar error.
  Error = Class.new(::StandardError)
  # Raised when a wrapped data stream class is not seekable.
  NonSeekableStream = Class.new(Error)
  # The exception raised when operations are performed on a stream that has
  # previously been closed.
  ClosedStream = Class.new(Error)
  # The exception raised when a filename exceeds 256 bytes in length, the
  # maximum supported by the standard Tar format.
  FileNameTooLong = Class.new(Error)
  # The exception raised when a data stream ends before the amount of data
  # expected in the archive's PosixHeader.
  UnexpectedEOF = Class.new(StandardError)
  # The exception raised when a file contains a relative path in secure mode
  # (the default for this version).
  SecureRelativePathError = Class.new(Error)
  # The exception raised when a file contains an invalid Posix header.
  InvalidTarStream = Class.new(Error)

  class << self
    # Tests if +path+ refers to a directory. Fixes an apparently
    # corrupted <tt>stat()</tt> call on Windows.
    def dir?(path)
      # rubocop:disable Style/CharacterLiteral
      File.directory?(path[-1] == ?/ ? path : "#{path}/")
      # rubocop:enable Style/CharacterLiteral
    end

    # A convenience method for wrapping Archive::Tar::Minitar::Input.open
    # (mode +r+) and Archive::Tar::Minitar::Output.open (mode +w+). No other
    # modes are currently supported.
    def open(dest, mode = 'r', &block)
      case mode
      when 'r'
        Input.open(dest, &block)
      when 'w'
        Output.open(dest, &block)
      else
        raise 'Unknown open mode for Archive::Tar::Minitar.open.'
      end
    end

    def const_missing(c) #:nodoc:
      case c
      when :BlockRequired
        warn 'This constant has been removed.'
        const_set(:BlockRequired, Class.new(StandardError))
      else
        super
      end
    end

    def windows? #:nodoc:
      RbConfig::CONFIG['host_os'] =~ /^(mswin|mingw|cygwin)/
    end

    # A convenience method to pack the file provided. +entry+ may either be a
    # filename (in which case various values for the file (see below) will be
    # obtained from <tt>File#stat(entry)</tt> or a Hash with the fields:
    #
    # <tt>:name</tt>::  The filename to be packed into the archive. Required.
    # <tt>:mode</tt>::  The mode to be applied.
    # <tt>:uid</tt>::   The user owner of the file. (Ignored on Windows.)
    # <tt>:gid</tt>::   The group owner of the file. (Ignored on Windows.)
    # <tt>:mtime</tt>:: The modification Time of the file.
    #
    # During packing, if a block is provided, #pack_file yields an +action+
    # Symol, the full name of the file being packed, and a Hash of
    # statistical information, just as with
    # Archive::Tar::Minitar::Input#extract_entry.
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
    # <tt>:name</tt>::    The filename to be packed into the tarchive.
    #                     *REQUIRED*.
    # <tt>:mode</tt>::    The mode to be applied.
    # <tt>:uid</tt>::     The user owner of the file. (+nil+ on Windows.)
    # <tt>:gid</tt>::     The group owner of the file. (+nil+ on Windows.)
    # <tt>:mtime</tt>::   The modification Time of the file.
    def pack_file(entry, outputter) #:yields action, name, stats:
      if outputter.kind_of?(Archive::Tar::Minitar::Output)
        outputter = outputter.tar
      end

      stats = {}

      if entry.kind_of?(Hash)
        name = entry[:name]
        entry.each { |kk, vv| stats[kk] = vv unless vv.nil? }
      else
        name = entry
      end

      name = name.sub(%r{\./}, '')
      stat = File.stat(name)
      stats[:mode]   ||= stat.mode
      stats[:mtime]  ||= stat.mtime
      stats[:size] = stat.size

      if windows?
        stats[:uid]  = nil
        stats[:gid]  = nil
      else
        stats[:uid]  ||= stat.uid
        stats[:gid]  ||= stat.gid
      end

      if File.file?(name)
        outputter.add_file_simple(name, stats) do |os|
          stats[:current] = 0
          yield :file_start, name, stats if block_given?
          File.open(name, 'rb') do |ff|
            until ff.eof?
              stats[:currinc] = os.write(ff.read(4096))
              stats[:current] += stats[:currinc]
              yield :file_progress, name, stats if block_given?
            end
          end
          yield :file_done, name, stats if block_given?
        end
      elsif dir?(name)
        yield :dir, name, stats if block_given?
        outputter.mkdir(name, stats)
      else
        raise %q(Don't yet know how to pack this type of file.)
      end
    end

    # A convenience method to pack files specified by +src+ into +dest+. If
    # +src+ is an Array, then each file detailed therein will be packed into
    # the resulting Archive::Tar::Minitar::Output stream; if +recurse_dirs+ is
    # true, then directories will be recursed.
    #
    # If +src+ is an Array, it will be treated as the result of Find.find; all
    # files matching will be packed.
    def pack(src, dest, recurse_dirs = true, &block)
      require 'find'
      Output.open(dest) do |outp|
        if src.kind_of?(Array)
          src.each do |entry|
            if dir?(entry) and recurse_dirs
              Find.find(entry) do |ee|
                pack_file(ee, outp, &block)
              end
            else
              pack_file(entry, outp, &block)
            end
          end
        else
          Find.find(src) do |entry|
            pack_file(entry, outp, &block)
          end
        end
      end
    end

    # A convenience method to unpack files from +src+ into the directory
    # specified by +dest+. Only those files named explicitly in +files+
    # will be extracted.
    def unpack(src, dest, files = [], options = {}, &block)
      Input.open(src) do |inp|
        if File.exist?(dest) and !dir?(dest)
          raise %q(Can't unpack to a non-directory.)
        end

        FileUtils.mkdir_p(dest) unless File.exist?(dest)

        inp.each do |entry|
          if files.empty? or files.include?(entry.full_name)
            inp.extract_entry(dest, entry, options, &block)
          end
        end
      end
    end

    # Check whether +io+ can seek without errors.
    def seekable?(io, methods = nil)
      # The IO class throws an exception at runtime if we try to change
      # position on a non-regular file.
      if io.respond_to?(:stat)
        io.stat.file?
      else
        # Duck-type the rest of this.
        methods ||= [ :pos, :pos=, :seek, :rewind ]
        methods = [ methods ] unless methods.kind_of?(Array)
        methods.all? { |m| io.respond_to?(m) }
      end
    end

    private

    def included(mod)
      return if modules.include?(mod)
      warn "Including #{self} has been deprecated (Minitar will become a class)."
      modules << mod
    end

    def modules
      require 'set'
      @modules ||= Set.new
    end
  end

  # This exists to make bytesize implementations work across Ruby versions.
  module ByteSize # :nodoc:
    private

    if ''.respond_to?(:bytesize)
      def bytesize(item)
        item.bytesize
      end
    else
      def bytesize(item)
        item.size
      end
    end
  end
end

require 'archive/tar/minitar/posix_header'
require 'archive/tar/minitar/input'
require 'archive/tar/minitar/output'
