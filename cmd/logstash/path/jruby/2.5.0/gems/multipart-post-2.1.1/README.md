# Multipart::Post

Adds a streamy multipart form post capability to `Net::HTTP`. Also supports other
methods besides `POST`.

[![Build Status](https://secure.travis-ci.org/socketry/multipart-post.svg)](http://travis-ci.org/socketry/multipart-post)

## Features/Problems

* Appears to actually work. A good feature to have.
* Encapsulates posting of file/binary parts and name/value parameter parts, similar to
  most browsers' file upload forms.
* Provides an `UploadIO` helper class to prepare IO objects for inclusion in the params
  hash of the multipart post object.

## Installation

    gem install multipart-post

or in your Gemfile

    gem 'multipart-post'

## Usage

```ruby
require 'net/http/post/multipart'

url = URI.parse('http://www.example.com/upload')
File.open("./image.jpg") do |jpg|
  req = Net::HTTP::Post::Multipart.new url.path,
    "file" => UploadIO.new(jpg, "image/jpeg", "image.jpg")
  res = Net::HTTP.start(url.host, url.port) do |http|
    http.request(req)
  end
end
```

To post multiple files or attachments, simply include multiple parameters with
`UploadIO` values:

```ruby
require 'net/http/post/multipart'

url = URI.parse('http://www.example.com/upload')
req = Net::HTTP::Post::Multipart.new url.path,
  "file1" => UploadIO.new(File.new("./image.jpg"), "image/jpeg", "image.jpg"),
  "file2" => UploadIO.new(File.new("./image2.jpg"), "image/jpeg", "image2.jpg")
res = Net::HTTP.start(url.host, url.port) do |http|
  http.request(req)
end
```

To post files with other normal, non-file params such as input values, you need to pass hashes to the `Multipart.new` method.

In Rails 4 for example:

```ruby
def model_params
  require_params = params.require(:model).permit(:param_one, :param_two, :param_three, :avatar)
  require_params[:avatar] = model_params[:avatar].present? ? UploadIO.new(model_params[:avatar].tempfile, model_params[:avatar].content_type, model_params[:avatar].original_filename) : nil
  require_params
end

require 'net/http/post/multipart'

url = URI.parse('http://www.example.com/upload')
Net::HTTP.start(url.host, url.port) do |http|
  req = Net::HTTP::Post::Multipart.new(url, model_params)
  key = "authorization_key"
  req.add_field("Authorization", key) #add to Headers
  http.use_ssl = (url.scheme == "https")
  http.request(req)
end
```

Or in plain ruby:

```ruby
def params(file)
  params = { "description" => "A nice picture!" }
  params[:datei] = UploadIO.new(file, "image/jpeg", "image.jpg")
  params
end

url = URI.parse('http://www.example.com/upload')
File.open("./image.jpg") do |file|
  req = Net::HTTP::Post::Multipart.new(url.path, params(file))
  res = Net::HTTP.start(url.host, url.port) do |http|
    return http.request(req).body
  end
end
```

### Debugging

You can debug requests and responses (e.g. status codes) for all requests by adding the following code:

```ruby
http = Net::HTTP.new(uri.host, uri.port)
http.set_debug_output($stdout)
```

## License

Released under the MIT license.

Copyright (c) 2007-2013 Nick Sieger <nick@nicksieger.com>  
Copyright, 2017, by [Samuel G. D. Williams](http://www.codeotaku.com/samuel-williams).

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
