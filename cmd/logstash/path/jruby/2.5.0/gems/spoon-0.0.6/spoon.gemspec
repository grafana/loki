Gem::Specification.new do |s|
  s.name = "spoon"
  s.version = "0.0.6"
  s.authors = ["Charles Oliver Nutter"]
  s.date = Time.now.strftime('%Y-%m-%d')
  s.description = s.summary = "Spoon is an FFI binding of the posix_spawn function (and Windows equivalent), providing fork+exec functionality in a single shot."
  s.files = `git ls-files`.lines.map(&:chomp)
  s.require_paths = ["lib"]
  s.add_dependency('ffi')
  s.license = "Apache-2.0"
end
