# rubyzip

[![Gem Version](https://badge.fury.io/rb/rubyzip.svg)](http://badge.fury.io/rb/rubyzip)
[![Build Status](https://secure.travis-ci.org/rubyzip/rubyzip.svg)](http://travis-ci.org/rubyzip/rubyzip)
[![Code Climate](https://codeclimate.com/github/rubyzip/rubyzip.svg)](https://codeclimate.com/github/rubyzip/rubyzip)
[![Coverage Status](https://img.shields.io/coveralls/rubyzip/rubyzip.svg)](https://coveralls.io/r/rubyzip/rubyzip?branch=master)

Rubyzip is a ruby library for reading and writing zip files.

## Important note

The Rubyzip interface has changed!!! No need to do `require "zip/zip"` and `Zip` prefix in class names removed.

If you have issues with any third-party gems that require an old version of rubyzip, you can use this workaround:

```ruby
gem 'rubyzip', '>= 1.0.0' # will load new rubyzip version
gem 'zip-zip' # will load compatibility for old rubyzip API.
```

## Requirements

- Ruby 1.9.2 or greater

## Installation

Rubyzip is available on RubyGems:

```
gem install rubyzip
```

Or in your Gemfile:

```ruby
gem 'rubyzip'
```

## Usage

### Basic zip archive creation

```ruby
require 'rubygems'
require 'zip'

folder = "Users/me/Desktop/stuff_to_zip"
input_filenames = ['image.jpg', 'description.txt', 'stats.csv']

zipfile_name = "/Users/me/Desktop/archive.zip"

Zip::File.open(zipfile_name, Zip::File::CREATE) do |zipfile|
  input_filenames.each do |filename|
    # Two arguments:
    # - The name of the file as it will appear in the archive
    # - The original file, including the path to find it
    zipfile.add(filename, File.join(folder, filename))
  end
  zipfile.get_output_stream("myFile") { |f| f.write "myFile contains just this" }
end
```

### Zipping a directory recursively

Copy from [here](https://github.com/rubyzip/rubyzip/blob/9d891f7353e66052283562d3e252fe380bb4b199/samples/example_recursive.rb)

```ruby
require 'zip'

# This is a simple example which uses rubyzip to
# recursively generate a zip file from the contents of
# a specified directory. The directory itself is not
# included in the archive, rather just its contents.
#
# Usage:
#   directory_to_zip = "/tmp/input"
#   output_file = "/tmp/out.zip"
#   zf = ZipFileGenerator.new(directory_to_zip, output_file)
#   zf.write()
class ZipFileGenerator
  # Initialize with the directory to zip and the location of the output archive.
  def initialize(input_dir, output_file)
    @input_dir = input_dir
    @output_file = output_file
  end

  # Zip the input directory.
  def write
    entries = Dir.entries(@input_dir) - %w[. ..]

    ::Zip::File.open(@output_file, ::Zip::File::CREATE) do |zipfile|
      write_entries entries, '', zipfile
    end
  end

  private

  # A helper method to make the recursion work.
  def write_entries(entries, path, zipfile)
    entries.each do |e|
      zipfile_path = path == '' ? e : File.join(path, e)
      disk_file_path = File.join(@input_dir, zipfile_path)

      if File.directory? disk_file_path
        recursively_deflate_directory(disk_file_path, zipfile, zipfile_path)
      else
        put_into_archive(disk_file_path, zipfile, zipfile_path)
      end
    end
  end

  def recursively_deflate_directory(disk_file_path, zipfile, zipfile_path)
    zipfile.mkdir zipfile_path
    subdir = Dir.entries(disk_file_path) - %w[. ..]
    write_entries subdir, zipfile_path, zipfile
  end

  def put_into_archive(disk_file_path, zipfile, zipfile_path)
    zipfile.add(zipfile_path, disk_file_path)
  end
end
```

### Save zip archive entries in sorted by name state

To save zip archives in sorted order like below, you need to set `::Zip.sort_entries` to `true`

```
Vegetable/
Vegetable/bean
Vegetable/carrot
Vegetable/celery
fruit/
fruit/apple
fruit/kiwi
fruit/mango
fruit/orange
```

After this, entries in the zip archive will be saved in ordered state.

### Default permissions of zip archives

On Posix file systems the default file permissions applied to a new archive
are (0666 - umask), which mimics the behavior of standard tools such as `touch`.

On Windows the default file permissions are set to 0644 as suggested by the
[Ruby File documentation](http://ruby-doc.org/core-2.2.2/File.html).

When modifying a zip archive the file permissions of the archive are preserved.

### Reading a Zip file

```ruby
MAX_SIZE = 1024**2 # 1MiB (but of course you can increase this)
Zip::File.open('foo.zip') do |zip_file|
  # Handle entries one by one
  zip_file.each do |entry|
    puts "Extracting #{entry.name}"
    raise 'File too large when extracted' if entry.size > MAX_SIZE

    # Extract to file or directory based on name in the archive
    entry.extract

    # Read into memory
    content = entry.get_input_stream.read
  end

  # Find specific entry
  entry = zip_file.glob('*.csv').first
  raise 'File too large when extracted' if entry.size > MAX_SIZE
  puts entry.get_input_stream.read
end
```

#### Notice about ::Zip::InputStream

`::Zip::InputStream` usable for fast reading zip file content because it not read Central directory.

But there is one exception when it is not working - General Purpose Flag Bit 3.

> If bit 3 (0x08) of the general-purpose flags field is set, then the CRC-32 and file sizes are not known when the header is written. The fields in the local header are filled with zero, and the CRC-32 and size are appended in a 12-byte structure (optionally preceded by a 4-byte signature) immediately after the compressed data

If `::Zip::InputStream` finds such entry in the zip archive it will raise an exception.

### Password Protection (Experimental)

Rubyzip supports reading/writing zip files with traditional zip encryption (a.k.a. "ZipCrypto"). AES encryption is not yet supported. It can be used with buffer streams, e.g.:

```ruby
Zip::OutputStream.write_buffer(::StringIO.new(''), Zip::TraditionalEncrypter.new('password')) do |out|
  out.put_next_entry("my_file.txt")
  out.write my_data
end.string
```

This is an experimental feature and the interface for encryption may change in future versions.

## Known issues

### Modify docx file with rubyzip

Use `write_buffer` instead `open`. Thanks to @jondruse

```ruby
buffer = Zip::OutputStream.write_buffer do |out|
  @zip_file.entries.each do |e|
    unless [DOCUMENT_FILE_PATH, RELS_FILE_PATH].include?(e.name)
      out.put_next_entry(e.name)
      out.write e.get_input_stream.read
     end
  end

  out.put_next_entry(DOCUMENT_FILE_PATH)
  out.write xml_doc.to_xml(:indent => 0).gsub("\n","")

  out.put_next_entry(RELS_FILE_PATH)
  out.write rels.to_xml(:indent => 0).gsub("\n","")
end

File.open(new_path, "wb") {|f| f.write(buffer.string) }
```

## Configuration

### Existing Files

By default, rubyzip will not overwrite files if they already exist inside of the extracted path. To change this behavior, you may specify a configuration option like so:

```ruby
Zip.on_exists_proc = true
```

If you're using rubyzip with rails, consider placing this snippet of code in an initializer file such as `config/initializers/rubyzip.rb`

Additionally, if you want to configure rubyzip to overwrite existing files while creating a .zip file, you can do so with the following:

```ruby
Zip.continue_on_exists_proc = true
```

### Non-ASCII Names

If you want to store non-english names and want to open them on Windows(pre 7) you need to set this option:

```ruby
Zip.unicode_names = true
```

Sometimes file names inside zip contain non-ASCII characters. If you can assume which encoding was used for such names and want to be able to find such entries using `find_entry` then you can force assumed encoding like so:

```ruby
Zip.force_entry_names_encoding = 'UTF-8'
```

Allowed encoding names are the same as accepted by `String#force_encoding`

### Date Validation

Some zip files might have an invalid date format, which will raise a warning. You can hide this warning with the following setting:

```ruby
Zip.warn_invalid_date = false
```

### Size Validation

**This setting defaults to `false` in rubyzip 1.3 for backward compatibility, but it will default to `true` in rubyzip 2.0.**

If you set
```
Zip.validate_entry_sizes = true
```
then `rubyzip`'s `extract` method checks that an entry's reported uncompressed size is not (significantly) smaller than its actual size. This is to help you protect your application against [zip bombs](https://en.wikipedia.org/wiki/Zip_bomb). Before `extract`ing an entry, you should check that its size is in the range you expect. For example, if your application supports processing up to 100 files at once, each up to 10MiB, your zip extraction code might look like:

```ruby
MAX_FILE_SIZE = 10 * 1024**2 # 10MiB
MAX_FILES = 100
Zip::File.open('foo.zip') do |zip_file|
  num_files = 0
  zip_file.each do |entry|
    num_files += 1 if entry.file?
    raise 'Too many extracted files' if num_files > MAX_FILES
    raise 'File too large when extracted' if entry.size > MAX_FILE_SIZE
    entry.extract
  end
end
```

If you need to extract zip files that report incorrect uncompressed sizes and you really trust them not too be too large, you can disable this setting with
```ruby
Zip.validate_entry_sizes = false
```

Note that if you use the lower level `Zip::InputStream` interface, `rubyzip` does *not* check the entry `size`s. In this case, the caller is responsible for making sure it does not read more data than expected from the input stream.

### Default Compression

You can set the default compression level like so:

```ruby
Zip.default_compression = Zlib::DEFAULT_COMPRESSION
```

It defaults to `Zlib::DEFAULT_COMPRESSION`. Possible values are `Zlib::BEST_COMPRESSION`, `Zlib::DEFAULT_COMPRESSION` and `Zlib::NO_COMPRESSION`

### Zip64 Support

By default, Zip64 support is disabled for writing. To enable it do this:

```ruby
Zip.write_zip64_support = true
```

_NOTE_: If you will enable Zip64 writing then you will need zip extractor with Zip64 support to extract archive.

### Block Form

You can set multiple settings at the same time by using a block:

```ruby
  Zip.setup do |c|
    c.on_exists_proc = true
    c.continue_on_exists_proc = true
    c.unicode_names = true
    c.default_compression = Zlib::BEST_COMPRESSION
  end
```

## Developing

To run the test you need to do this:

```
bundle install
rake
```

## Website and Project Home

http://github.com/rubyzip/rubyzip

http://rdoc.info/github/rubyzip/rubyzip/master/frames

## Authors

Alexander Simonov ( alex at simonov.me)

Alan Harper ( alan at aussiegeek.net)

Thomas Sondergaard (thomas at sondergaard.cc)

Technorama Ltd. (oss-ruby-zip at technorama.net)

extra-field support contributed by Tatsuki Sugiura (sugi at nemui.org)

## License

Rubyzip is distributed under the same license as ruby. See
http://www.ruby-lang.org/en/LICENSE.txt
