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

require 'spoon'

#
# Do a recursive ls on the current directory, redirecting output to /tmp/ls.out
#

file_actions = Spoon::FileActions.new
file_actions.close(1)
file_actions.open(1, "/tmp/ls.out", File::WRONLY | File::TRUNC | File::CREAT, 0600)
spawn_attr = Spoon::SpawnAttributes.new
pid = Spoon.posix_spawn('/usr/bin/env', file_actions, spawn_attr, %w(env ls -R))

Process.waitpid(pid)
