#!/usr/bin/env ruby

require 'minitar'
require 'minitest_helper'

class TestTarWriter < Minitest::Test
  include Archive::Tar::Minitar::ByteSize

  class DummyIO
    include Archive::Tar::Minitar::ByteSize

    attr_reader :data

    def initialize
      reset
    end

    def write(dat)
      data << dat
      dat.size
    end

    def reset
      @data = ''
      @data.force_encoding('ascii-8bit') if @data.respond_to?(:force_encoding)
    end
  end

  def setup
    @data = 'a' * 10
    @unicode = [0xc3.chr, 0xa5.chr].join * 10
    @unicode.force_encoding('utf-8') if @unicode.respond_to?(:force_encoding)
    @dummyos = DummyIO.new
    @os = Minitar::Writer.new(@dummyos)
  end

  def teardown
    @os.close
  end

  def test_open_no_block
    writer = Minitar::Writer.open(@dummyos)
    refute writer.closed?
  ensure
    writer.close
    assert writer.closed?
  end


  def test_add_file_simple
    @dummyos.reset

    Minitar::Writer.open(@dummyos) do |os|
      os.add_file_simple('lib/foo/bar', :mode => 0o644, :size => 10) do |f|
        f.write 'a' * 10
      end
      os.add_file_simple('lib/bar/baz', :mode => 0o644, :size => 100) do |f|
        f.write 'fillme'
      end
    end

    assert_headers_equal(tar_file_header('lib/foo/bar', '', 0o644, 10),
      @dummyos.data[0, 512])
    assert_equal('a' * 10 + "\0" * 502, @dummyos.data[512, 512])
    assert_headers_equal(tar_file_header('lib/bar/baz', '', 0o644, 100),
      @dummyos.data[512 * 2, 512])
    assert_equal('fillme' + "\0" * 506, @dummyos.data[512 * 3, 512])
    assert_equal("\0" * 512, @dummyos.data[512 * 4, 512])
    assert_equal("\0" * 512, @dummyos.data[512 * 5, 512])
  end

  def test_write_operations_fail_after_closed
    @dummyos.reset
    @os.add_file_simple('sadd', :mode => 0o644, :size => 20) { |f| }
    @os.close
    assert_raises(Minitar::ClosedStream) { @os.flush }
    assert_raises(Minitar::ClosedStream) {
      @os.add_file('dfdsf', :mode => 0o644) {}
    }
    assert_raises(Minitar::ClosedStream) { @os.mkdir 'sdfdsf', :mode => 0o644 }
  end

  def test_file_name_is_split_correctly
    # test insane file lengths, and: a{100}/b{155}, etc
    @dummyos.reset
    names = [
      "#{'a' * 155}/#{'b' * 100}",
      "#{'a' * 151}/#{'qwer/' * 19}bla",
      "/#{'a' * 49}/#{'b' * 50}",
      "#{'a' * 49}/#{'b' * 50}x",
      "#{'a' * 49}x/#{'b' * 50}"
    ]
    o_names = [
      'b' * 100,
      "#{'qwer/' * 19}bla",
      'b' * 50,
      "#{'b' * 50}x",
      'b' * 50
    ]
    o_prefixes = [
      'a' * 155,
      'a' * 151,
      "/#{'a' * 49}",
      'a' * 49,
      "#{'a' * 49}x"
    ]
    names.each do |name|
      @os.add_file_simple(name, :mode => 0o644, :size => 10) {}
    end
    names.each_index do |i|
      assert_headers_equal(
        tar_file_header(o_names[i], o_prefixes[i], 0o644, 10),
        @dummyos.data[2 * i * 512, 512]
      )
    end
  end

  def test_file_name_is_long
    @dummyos.reset

    @os.add_file_simple(File.join('a' * 152, 'b' * 10, 'c' * 92),
                        :mode => 0o644, :size => 10) {}
    @os.add_file_simple(File.join('d' * 162, 'e' * 10),
                        :mode => 0o644, :size => 10) {}
    @os.add_file_simple(File.join('f' * 10, 'g' * 110),
                        :mode => 0o644, :size => 10) {}
    # Issue #6.
    @os.add_file_simple('a' * 114, :mode => 0o644, :size => 10) {}

    # "././@LongLink", a file name, its actual header, its data, ...
    4.times do |i|
      assert_equal(Minitar::PosixHeader::GNU_EXT_LONG_LINK,
                   @dummyos.data[4 * i * 512, 32].rstrip)
    end
  end

  def test_add_file
    dummyos = StringIO.new
    def dummyos.method_missing(meth, *a) # rubocop:disable Style/MethodMissing
      string.send(meth, *a)
    end
    content1 = ('a'..'z').to_a.join('') # 26
    content2 = ('aa'..'zz').to_a.join('') # 1352
    Minitar::Writer.open(dummyos) do |os|
      os.add_file('lib/foo/bar', :mode => 0o644) { |f, _opts| f.write 'a' * 10 }
      os.add_file('lib/bar/baz', :mode => 0o644) { |f, _opts| f.write content1 }
      os.add_file('lib/bar/baz', :mode => 0o644) { |f, _opts| f.write content2 }
      os.add_file('lib/bar/baz', :mode => 0o644) { |_f, _opts| }
    end
    assert_headers_equal(tar_file_header('lib/foo/bar', '', 0o644, 10),
      dummyos[0, 512])
    assert_equal(%Q(#{'a' * 10}#{"\0" * 502}), dummyos[512, 512])
    offset = 512 * 2
    [content1, content2, ''].each do |data|
      assert_headers_equal(tar_file_header('lib/bar/baz', '', 0o644,
        bytesize(data)), dummyos[offset, 512])
      offset += 512
      until !data || data == ''
        chunk = data[0, 512]
        data[0, 512] = ''
        assert_equal(chunk + "\0" * (512 - bytesize(chunk)),
          dummyos[offset, 512])
        offset += 512
      end
    end
    assert_equal("\0" * 1024, dummyos[offset, 1024])
  end

  def test_add_file_tests_seekability
    assert_raises(Archive::Tar::Minitar::NonSeekableStream) do
      @os.add_file('libdfdsfd', :mode => 0o644) { |f| }
    end
  end

  def test_write_header
    @dummyos.reset
    @os.add_file_simple('lib/foo/bar', :mode => 0o644, :size => 0) {}
    @os.flush
    assert_headers_equal(tar_file_header('lib/foo/bar', '', 0o644, 0),
      @dummyos.data[0, 512])
    @dummyos.reset
    @os.mkdir('lib/foo', :mode => 0o644)
    assert_headers_equal(tar_dir_header('lib/foo', '', 0o644),
      @dummyos.data[0, 512])
    @os.mkdir('lib/bar', :mode => 0o644)
    assert_headers_equal(tar_dir_header('lib/bar', '', 0o644),
      @dummyos.data[512 * 1, 512])
  end

  def test_write_data
    @dummyos.reset
    @os.add_file_simple('lib/foo/bar', :mode => 0o644, :size => 10) do |f|
      f.write @data
    end
    @os.flush
    assert_equal(@data + ("\0" * (512 - bytesize(@data))),
      @dummyos.data[512, 512])
  end

  def test_write_unicode_data
    @dummyos.reset

    if @unicode.respond_to?(:bytesize)
      assert_equal 10, @unicode.size
      assert_equal 20, @unicode.bytesize
      @unicode.force_encoding('ascii-8bit')
    end

    file = ['lib/foo/b', 0xc3.chr, 0xa5.chr, 'r'].join

    @os.add_file_simple(file, :mode => 0o644, :size => 20) do |f|
      f.write @unicode
    end
    @os.flush
    assert_equal(@unicode + ("\0" * (512 - bytesize(@unicode))),
      @dummyos.data[512, 512])
  end

  def test_file_size_is_checked
    @dummyos.reset
    assert_raises(Minitar::Writer::WriteBoundaryOverflow) do
      @os.add_file_simple('lib/foo/bar', :mode => 0o644, :size => 10) do |f|
        f.write '1' * 100
      end
    end
    @os.add_file_simple('lib/foo/bar', :mode => 0o644, :size => 10) { |f| }
  end
end
