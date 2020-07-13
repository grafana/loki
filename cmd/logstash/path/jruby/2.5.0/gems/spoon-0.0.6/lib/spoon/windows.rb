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

# Windows _spawnv

require 'ffi'

module Spoon
  P_NOWAIT = 1
  extend FFI::Library

  ffi_lib FFI::Library::LIBC
  attach_function :_spawnve, [:int, :string, :pointer, :pointer], :int
  attach_function :_spawnvpe, [:int, :string, :pointer, :pointer], :int
  
  ffi_lib 'kernel32'
  ffi_convention :stdcall
  attach_function :_get_process_id, :GetProcessId, [:int], :ulong
  
  def self.spawn(*args)
    spawn_args = _prepare_spawn_args(args)
    _get_process_id(_spawnve(*spawn_args))
  end
  
  def self.spawnp(*args)
    spawn_args = _prepare_spawn_args(args)
    _get_process_id(_spawnvpe(*spawn_args))
  end
  
  private
  
  def self._prepare_spawn_args(args)
    args_ary = FFI::MemoryPointer.new(:pointer, args.length + 1)
    str_ptrs = args.map {|str| FFI::MemoryPointer.from_string(str)}
    args_ary.put_array_of_pointer(0, str_ptrs)

    env_ary = FFI::MemoryPointer.new(:pointer, ENV.length + 1)
    env_ptrs = ENV.map {|key,value| FFI::MemoryPointer.from_string("#{key}=#{value}")}
    env_ary.put_array_of_pointer(0, env_ptrs)
    
    [P_NOWAIT, args[0], args_ary, env_ary]
  end
end
