# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# UNIX posix_spawn

require 'ffi'

module Spoon
  class FileActions
    attr_reader :pointer
    SIZE = FFI::Platform.mac? ? FFI.type_size(:pointer) : 128

    def initialize
      @pointer =  FFI::AutoPointer.new(LibC.malloc(SIZE), Releaser)
      error = LibC.posix_spawn_file_actions_init(@pointer)
      raise SystemCallError.new("posix_file_actions_init", error) unless error == 0
    end

    class Releaser
      def self.call(ptr)
        LibC.posix_spawn_file_actions_destroy(ptr)
        LibC.free(ptr)
      end
    end

    def open(fd, path, oflag, mode)
      error = LibC.posix_spawn_file_actions_addopen(@pointer, fd, path, oflag, mode)
      raise SystemCallError.new("posix_file_actions_addopen", error) unless error == 0
      self
    end

    def close(fd)
      error = LibC.posix_spawn_file_actions_addclose(@pointer, fd)
      raise SystemCallError.new("posix_file_actions_addclose", error) unless error == 0
      self
    end

    def dup2(fd, newfd)
      error = LibC.posix_spawn_file_actions_adddup2(@pointer, fd, newfd)
      raise SystemCallError.new("posix_file_actions_adddup2", error) unless error == 0
      self
    end
  end

  class SpawnAttributes
    attr_reader :pointer
    SIZE = FFI::Platform.mac? ? FFI.type_size(:pointer) : 512

    def initialize
      @pointer =  FFI::AutoPointer.new(LibC.malloc(SIZE), Releaser)
      error = LibC.posix_spawnattr_init(@pointer)
      raise SystemCallError.new("posix_spawnattr_init", error) unless error == 0
    end

    class Releaser
      def self.call(ptr)
        LibC.posix_spawnattr_destroy(ptr)
        LibC.free(ptr)
      end
    end

    def pgroup=(group)
      error = LibC.posix_spawnattr_setpgroup(pointer, group)
      raise SystemCallError.new("posix_spawnattr_setpgroup", error) unless error == 0
      group
    end

    def pgroup
      group = FFI::MemoryPointer.new :pid_t
      error = LibC.posix_spawnattr_getpgroup(pointer, group)
      raise SystemCallError.new("posix_spawnattr_getpgroup", error) unless error == 0
      get_pid(group)
    end
  end

  def self.posix_spawn(path, file_actions, spawn_attr, argv, env = ENV)
    pid_ptr, argv_ptr, env_ptr = _prepare_spawn_args(argv, env)
    error = LibC.posix_spawnp(pid_ptr, path, file_actions, spawn_attr, argv_ptr, env_ptr)
    raise SystemCallError.new(path, error) unless error == 0
    get_pid(pid_ptr)
  end

  def self.posix_spawnp(file, file_actions, spawn_attr, argv, env = ENV)
    pid_ptr, argv_ptr, env_ptr = _prepare_spawn_args(argv, env)
    error = LibC.posix_spawnp(pid_ptr, file, file_actions, spawn_attr, argv_ptr, env_ptr)
    raise SystemCallError.new(file, error) unless error == 0
    get_pid(pid_ptr)
  end

  def self.spawn(*args)
    posix_spawn(args[0], nil, nil, args, ENV)
  end

  def self.spawnp(*args)
    posix_spawnp(args[0], nil, nil, args, ENV)
  end
  
  private

  class PointerArray
    def initialize
      @ary = []
    end

    def <<(ptr)
      @ary << ptr
      self
    end

    def pointer
      if @pointer.nil? || (@pointer.size / @pointer.type_size) <= @ary.length
	ptr = FFI::MemoryPointer.new(:pointer, @ary.length + 1)
	ptr.put_array_of_pointer(0, @ary)
	@pointer = ptr
      end
      @pointer
    end
  end

  if FFI.type_size(:pid_t) == 4
    def self.get_pid(ptr)
      ptr.get_int32(0)
    end
  else
    def self.get_pid(ptr)
      ptr.get_int64(0)
    end
  end

  module LibC
    extend FFI::Library
    ffi_lib FFI::Library::LIBC

    class PointerConverter
      extend FFI::DataConverter
      native_type FFI::Type::POINTER

      def self.to_native(value, ctx)
	value ? value.pointer : nil
      end
    end

    typedef PointerConverter, :file_actions
    typedef PointerConverter, :spawn_attr
    typedef PointerConverter, :ptr_array

    attach_function :posix_spawn, [:pointer, :string, :file_actions, :spawn_attr, :ptr_array, :ptr_array ], :int
    attach_function :posix_spawnp, [:pointer, :string, :file_actions, :spawn_attr, :ptr_array, :ptr_array ], :int
    attach_function :posix_spawn_file_actions_init, [ :pointer ], :int
    attach_function :posix_spawn_file_actions_destroy, [ :pointer ], :int
    attach_function :posix_spawn_file_actions_adddup2, [ :pointer, :int, :int ], :int
    attach_function :posix_spawn_file_actions_addclose, [ :pointer, :int ], :int
    attach_function :posix_spawn_file_actions_addopen, [ :pointer, :int, :string, :int, :mode_t ], :int
    attach_function :posix_spawnattr_init, [ :pointer ], :int
    attach_function :posix_spawnattr_destroy, [ :pointer ], :int
    attach_function :posix_spawnattr_setpgroup, [ :pointer, :pid_t ], :int
    attach_function :posix_spawnattr_getpgroup, [ :pointer, :pointer ], :int
    attach_function :malloc, [ :size_t ], :pointer
    attach_function :free, [ :pointer ], :void
    attach_function :strerror, [ :int ], :string
  end

  def self._prepare_spawn_args(argv, env)
    pid_ptr = FFI::MemoryPointer.new(:pid_t, 1)

    args_ary = argv.inject(PointerArray.new) { |ary, str| ary << FFI::MemoryPointer.from_string(str) }
    env_ary = PointerArray.new
    env.each_pair { |key, value| env_ary << FFI::MemoryPointer.from_string("#{key}=#{value}") }

    [pid_ptr, args_ary, env_ary]
  end
end
