# frozen_string_literal: true

module TarTestHelpers
  include Archive::Tar::Minitar::ByteSize

  Field = Struct.new(:name, :offset, :length)
  def self.Field(name, length) # rubocop:disable Style/MethodName
    @offset ||= 0
    field = Field.new(name, @offset, length)
    @offset += length
    FIELDS[name] = field
    FIELD_ORDER << name
    field
  end

  private

  FIELDS = {} # rubocop:disable Style/MutableConstant
  FIELD_ORDER = [] # rubocop:disable Style/MutableConstant

  Field('name', 100)
  Field('mode', 8)
  Field('uid', 8)
  Field('gid', 8)
  Field('size', 12)
  Field('mtime', 12)
  Field('checksum', 8)
  Field('typeflag', 1)
  Field('linkname', 100)
  Field('magic', 6)
  Field('version', 2)
  Field('uname', 32)
  Field('gname', 32)
  Field('devmajor', 8)
  Field('devminor', 8)
  Field('prefix', 155)

  BLANK_CHECKSUM = ' ' * 8
  NULL_100 = "\0" * 100
  USTAR = "ustar\0".freeze
  DOUBLE_ZERO = '00'.freeze

  def assert_headers_equal(expected, actual)
    FIELD_ORDER.each do |field|
      message = if field == 'checksum'
                  'Header checksums are expected to match.'
                else
                  "Header field #{field} is expected to match."
                end

      offset = FIELDS[field].offset
      length = FIELDS[field].length

      assert_equal(expected[offset, length], actual[offset, length], message)
    end
  end

  def assert_modes_equal(expected, actual, name)
    return if Minitar.windows?

    assert_equal(
      mode_string(expected),
      mode_string(actual),
      "Mode for #{name} does not match"
    )
  end

  def tar_file_header(fname, dname, mode, length)
    update_checksum(header('0', fname, dname, length, mode))
  end

  def tar_dir_header(name, prefix, mode)
    update_checksum(header('5', name, prefix, 0, mode))
  end

  def header(type, fname, dname, length, mode)
    raw_header(type,
      asciiz(fname, 100),
      asciiz(dname, 155),
      z(to_oct(length, 11)),
      z(to_oct(mode, 7))
    )
  end

  def raw_header(type, fname, dname, length, mode)
    arr = [
      fname, mode, z(to_oct(nil, 7)), z(to_oct(nil, 7)),
      length, z(to_oct(0, 11)), BLANK_CHECKSUM, type,
      NULL_100, USTAR, DOUBLE_ZERO, asciiz('', 32), asciiz('', 32),
      z(to_oct(nil, 7)), z(to_oct(nil, 7)), dname
    ]

    h = arr.join.bytes.to_a.pack('C100C8C8C8C12C12C8CC100C6C2C32C32C8C8C155')
    ret = h + "\0" * (512 - bytesize(h))
    assert_equal(512, bytesize(ret))
    ret
  end

  def update_checksum(header)
    header[FIELDS['checksum'].offset, FIELDS['checksum'].length] =
      # inject(:+) was introduced in which version?
      sp(z(to_oct(header.unpack('C*').inject { |a, e| a + e }, 6)))
    header
  end

  def to_oct(n, pad_size)
    if n.nil?
      "\0" * pad_size
    else
      "%0#{pad_size}o" % n
    end
  end

  def asciiz(str, length)
    str + "\0" * (length - bytesize(str))
  end

  def sp(s)
    s + ' '
  end

  def z(s)
    s + "\0"
  end

  def mode_string(value)
    '%04o' % (value & 0o777)
  end

  Minitest::Test.send(:include, self)
end
