gemspec = Gem::Specification.new do |s|
  s.name              = 'example'
  s.version           = '0.0.3'
  s.summary           = 'summary'
  s.description       = 'description'
  s.files             = Dir.glob("{lib,test}/**/*")
  s.require_path      = 'lib'
  s.has_rdoc          = true
  s.homepage          = 'http://example.com'
  s.authors           = ['Luther Blisset']
  s.email             = 'user@example.com'
end
