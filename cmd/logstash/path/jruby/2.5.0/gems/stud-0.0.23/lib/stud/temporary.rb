require "securerandom" # for uuid generation
require "fileutils"

module Stud
  module Temporary
    DEFAULT_PREFIX = "studtmp"

    # Returns a string for a randomly-generated temporary path.
    #
    # This does not create any files.
    def pathname(prefix=DEFAULT_PREFIX)

      root = ENV["TMP"] || ENV["TMPDIR"] || ENV["TEMP"] || "/tmp"
      return File.join(root, "#{prefix}-#{SecureRandom.hex(30)}")
    end

    # Return a File handle to a randomly-generated path.
    #
    # Any arguments beyond the first (prefix) argument will be
    # given to File.new.
    #
    # If no file args are given, the default file mode is "w+"
    def file(prefix=DEFAULT_PREFIX, *args, &block)
      args << "w+" if args.empty?
      file = File.new(pathname(prefix), *args)
      if block_given?
        begin
          block.call(file)
        ensure
          file.close unless file.closed?
          File.unlink(file.path)
        end
      else
        return file
      end
    end

    # Make a temporary directory.
    #
    # If given a block, the directory path is given to the block.  WHen the
    # block finishes, the directory and all its contents will be deleted.
    #
    # If no block given, it will return the path to a newly created directory.
    # You are responsible for then cleaning up.
    def directory(prefix=DEFAULT_PREFIX, &block)
      path = pathname(prefix)
      Dir.mkdir(path)

      if block_given?
        begin
          block.call(path)
        ensure
          FileUtils.rm_r(path)
        end
      else
        return path
      end
    end
    extend self
  end # module Temporary

end # module Stud

