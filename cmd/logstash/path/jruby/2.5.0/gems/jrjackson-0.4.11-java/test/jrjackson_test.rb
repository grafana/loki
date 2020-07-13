# encoding: UTF-8

$LOAD_PATH << File.expand_path('../../lib', __FILE__)

require "java"

require 'securerandom'

require 'test/unit'
require 'thread'
require 'bigdecimal'
require 'jrjackson'
require 'stringio'
require 'time'
require 'date'

class JrJacksonTest < Test::Unit::TestCase
  class Test::Unit::CustomObj
  end

  class Test::Unit::NullObj < BasicObject
    def nil?() true; end
  end

  class ToJsonData
    attr_reader :one, :two, :six
    def initialize(a,b,c)
      @one, @two, @six = a,b,c
    end
    def to_h
      {'one' => one, 'two' => two, 'six' => six}
    end
    def to_json_data
      [one, two, six]
    end
  end

  class CustomToH
    attr_accessor :one, :two, :six
    def initialize(a,b,c)
      @one, @two, @six = a,b,c
    end
    def to_h
      {'one' => one, 'two' => two, 'six' => six}
    end
  end

  class CustomToHash
    attr_accessor :one, :two, :six
    def initialize(a,b,c)
      @one, @two, @six = a,b,c
    end
    def to_hash
      {'one' => one, 'two' => two, 'six' => six}
    end
  end

  class CustomToJson
    attr_accessor :one, :two, :six
    def initialize(a,b,c)
      @one, @two, @six = a,b,c
    end
    def to_json
      %Q|{"one":#{one},"two":#{two},"six":#{six}}|
    end
  end

  class CustomToTime
    def initialize(tm = Time.now)
      @now = tm
    end
    def to_time
      @now.to_time.utc
    end
  end

  CustomStruct = Struct.new(:one, :two, :six)

  class StrangeError < RuntimeError

  end

  class ScHandler
    attr_accessor :calls

    def initialize(arr = [])
      @calls = arr
    end

    def hash_start()
      @calls << [:hash_start]
      {}
    end

    def hash_end()
      @calls << [:hash_end]
    end

    def hash_key(key)
      @calls << [:hash_key, key]
      return 'too' if 'two' == key
      return :symbol if 'symbol' == key
      key
    end

    def array_start()
      @calls << [:array_start]
      []
    end

    def array_end()
      @calls << [:array_end]
    end

    def add_value(value)
      @calls << [:add_value, value]
    end

    def hash_set(h, key, value)
      # h[key] = value
      @calls << [:hash_set, key, value]
    end

    def array_append(a, value)
      # a.push(value)
      @calls << [:array_append, value]
    end
  end

  JsonString = %Q|{
    "array": [
      {
        "num"   : 3,
        "string": "message",
        "hash"  : {
          "h2"  : {
            "a" : [ 1, 2, 3 ]
          }
        }
      }
    ],
    "boolean" : true
  }|

  def test_sc_parse
    array = []
    handler = ScHandler.new(array)
    JrJackson::Json.sc_load(handler, JsonString)
    assert_equal(
      [
        [:hash_start],
        [:array_start],
        [:hash_start],
        [:hash_key, 'num'],
        [:hash_set, "num", 3],
        [:hash_key, 'string'],
        [:hash_set, "string", "message"],
        [:hash_start],
        [:hash_start],
        [:array_start],
        [:array_append, 1],
        [:array_append, 2],
        [:array_append, 3],
        [:array_end],
        [:hash_key, "a"],
        [:hash_set, "a", []],
        [:hash_end],
        [:hash_key, "h2"],
        [:hash_set, "h2", {}],
        [:hash_end],
        [:hash_key, "hash"],
        [:hash_set, "hash", {}],
        [:hash_end],
        [:array_append, {}],
        [:array_end],
        [:hash_key, "array"],
        [:hash_set, "array", []],
        [:hash_key, 'boolean'],
        [:hash_set, "boolean", true],
        [:hash_end],
        [:add_value, {}]
      ],
    handler.calls
  )
  end

  class SjHandler
    attr_reader :calls
    def initialize(arr = []) @calls = arr; end
    def hash_start(key) @calls << [:hash_start, key]; end
    def hash_end(key) @calls << [:hash_end, key]; end
    def array_start(key) @calls << [:array_start, key]; end
    def array_end(key) @calls << [:array_end, key]; end
    def add_value(value, key) @calls << [:add_value, value, key]; end
    def error(message, line, column) @calls << [:error, message, line, column]; end
  end

  def test_sj_parse
    handler = SjHandler.new()
    JrJackson::Json.sj_load(handler, JsonString)
    assert_equal(
      [
        [:hash_start, nil],
        [:array_start, 'array'],
        [:hash_start, nil],
        [:add_value, 3, 'num'],
        [:add_value, 'message', 'string'],
        [:hash_start, 'hash'],
        [:hash_start, 'h2'],
        [:array_start, 'a'],
        [:add_value, 1, nil],
        [:add_value, 2, nil],
        [:add_value, 3, nil],
        [:array_end, 'a'],
        [:hash_end, 'h2'],
        [:hash_end, 'hash'],
        [:hash_end, nil],
        [:array_end, 'array'],
        [:add_value, true, 'boolean'],
        [:hash_end, nil]
      ],
      handler.calls
    )
  end

  def test_to_json_data
    object = ToJsonData.new("uno", :two, 6.0)
    expected = "[1,[\"uno\",\"two\",6.0]]"
    actual = JrJackson::Json.dump([1, object])
    assert_equal expected, actual
  end

  def test_datetime
    h = {datetime: DateTime.parse("2014-01-27T18:24:46+01:00")}
    expected = '{"datetime":"2014-01-27 17:24:46 +0000"}'
    actual = JrJackson::Json.dump(h)
    assert_equal expected, actual
  end

  def test_dump_date_in_array
    expected = "[\"2016-04-10\"]"
    td = Date.new(2016, 4, 10)
    actual = JrJackson::Json.generate([td])
    assert_equal(actual, expected)
  end

  def test_threading
    q1, q2, q3 = Queue.new, Queue.new, Queue.new

    s1 = %Q|{"a":2.5, "b":0.214, "c":3.4567, "d":-3.4567}|
    th1 = Thread.new(s1) do |string|
      q1 << JrJackson::Json.load(string, {use_bigdecimal: true, raw: true})
    end
    th2 = Thread.new(s1) do |string|
      q2 << JrJackson::Json.load(string, {use_bigdecimal: true, symbolize_keys: true})
    end
    th3 = Thread.new(s1) do |string|
      q3 << JrJackson::Json.load(string, {use_bigdecimal: false, symbolize_keys: true})
    end
    a1, a2, a3 = q1.pop, q2.pop, q3.pop
    assert_equal ["a", "b", "c", "d"], a1.keys
    assert a1.values.all? {|v| v.is_a?(Java::JavaMath::BigDecimal)}, "Expected all values to be Java::JavaMath::BigDecimal"
    assert_equal [:a, :b, :c, :d], a2.keys
    assert a2.values.all? {|v| v.is_a?(BigDecimal)}, "Expected all values to be BigDecimal"
    assert a3.values.all? {|v| v.is_a?(Float)}, "Expected all values to be Float"
  end

  def test_deserialize_JSON_with_UTF8_characters
    json_string = JrJackson::Json.dump({"utf8" => "żółć"})
    expected = {utf8: "żółć"}
    actual = JrJackson::Json.load(json_string, :symbolize_keys => true)

    assert_equal expected, actual
  end

  def test_deserialize_JSON_with_two_entries
    json_string = JrJackson::Json.dump({'foo' => 'foo_value', 'bar' => 'bar_value'})
    expected = {foo: 'foo_value', bar: 'bar_value'}
    actual = JrJackson::Json.load(json_string, :symbolize_keys => true)
    assert_equal expected, actual
  end

  def test_serialize_non_json_datatypes_as_values
    dt = Time.now.utc
    today = Date.today
    co1 = CustomToH.new("uno", :two, 6.0)
    co2 = CustomToHash.new("uno", :two, 6.0)
    co3 = CustomToJson.new(1.0, 2, 6.0)
    co4 = CustomStruct.new(1, 2, 6)
    co5 = CustomToTime.new(today)
    source = {'sym' => :a_symbol, 'dt' => dt, 'co1' => co1, 'co2' => co2, 'co3' => co3, 'co4' => co4, 'co5' => co5}
    json_string = JrJackson::Json.dump(source)
    expected = {
      :sym => "a_symbol",
      :dt => dt.strftime('%F %T %z'),
      :co1 => {:one => "uno", :two => "two", :six => 6.0 },
      :co2 => {:one => "uno", :two => "two", :six => 6.0 },
      :co3 => {:one => 1.0, :two => 2.0, :six => 6.0 },
      :co4 => [1, 2, 6],
      :co5 => today.to_time.utc.strftime('%F %T %z')
    }
    actual = JrJackson::Json.load(json_string, :symbolize_keys => true)
    assert_equal expected, actual
  end

  def test_raw_serialize_base_classes
    # String
    assert_equal JrJackson::Json.dump("foo"), "\"foo\""

    # Hash and implementations of the Java Hash interface
    assert_equal JrJackson::Json.dump({"foo" => 1}), "{\"foo\":1}"
    assert_equal JrJackson::Json.dump(Java::JavaUtil::HashMap.new({"foo" => 1})), "{\"foo\":1}"
    assert_equal JrJackson::Json.dump(Java::JavaUtil::LinkedHashMap.new({"foo" => 1})), "{\"foo\":1}"

    # Array and implementations of the Java List interface
    assert_equal JrJackson::Json.dump(["foo", 1]), "[\"foo\",1]"
    assert_equal JrJackson::Json.dump(Java::JavaUtil::ArrayList.new(["foo", 1])), "[\"foo\",1]"
    assert_equal JrJackson::Json.dump(Java::JavaUtil::LinkedList.new(["foo", 1])), "[\"foo\",1]"
    assert_equal JrJackson::Json.dump(Java::JavaUtil::Vector.new(["foo", 1])), "[\"foo\",1]"

    # true/false
    assert_equal JrJackson::Json.dump(true), "true"
    assert_equal JrJackson::Json.dump(false), "false"

    # nil
    assert_equal JrJackson::Json.dump(nil), "null"
  end

  def test_serialize_date
    # default date format
    time_string = "2014-06-10 18:18:40 EDT"
    source_time = Time.parse(time_string)
    serialized_output = JrJackson::Json.dump({"time" => source_time})
    other_time = Time.parse(serialized_output.split('"')[-2])
    assert_equal other_time.to_f, source_time.to_f
  end

  def test_serialize_date_date_format

    time = Time.new(2014,6,10,18,18,40, "-04:00")
    # using date_format option
    assert_equal "{\"time\":\"2014-06-10\"}", JrJackson::Json.dump({"time" => time}, :date_format => "yyyy-MM-dd")
    assert_match(/\{"time"\:"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}[+-]\d{4}"\}/, JrJackson::Json.dump({"time" => time}, :date_format => "yyyy-MM-dd'T'HH:mm:ss.SSSZ"))
  end

  def test_serialize_date_date_format_timezone

    time = Time.new(2014,6,10,18,18,40, "-04:00")
    # using date_format and timezone options
    assert_equal "{\"time\":\"2014-06-10T22:18:40.000+0000\"}", JrJackson::Json.dump({"time" => time}, :date_format => "yyyy-MM-dd'T'HH:mm:ss.SSSZ", :timezone => "UTC")
    # iso8601 date_format and timezone
    assert_equal "{\"time\":\"2014-06-10T22:18:40.000Z\"}", JrJackson::Json.dump({"time" => time}, :date_format => "yyyy-MM-dd'T'HH:mm:ss.SSSX", :timezone => "UTC")
  end

  def test_can_parse_returning_java_objects
    expected = {"arr"=>[2, 3, 4],
      "flo"=>0.333E1,
      "moo"=>"bar",
      "utf8"=>"żółć",
      "zzz"=>{"bar"=>-9}}
    json = '{"utf8":"żółć", "moo": "bar", "zzz": {"bar":-9}, "arr": [2,3,4], "flo": 3.33}'

    actual = JrJackson::Json.parse_java(json)

    assert_equal Java::JavaUtil::HashMap, actual.class
    assert_equal Java::JavaUtil::ArrayList, actual["arr"].class
    assert_equal Java::JavaMath::BigDecimal, actual["flo"].class
    assert_equal "3.33", actual["flo"].to_s
    assert_equal "bar", actual["moo"]
    assert_equal "żółć", actual["utf8"]
    assert_equal Java::JavaUtil::HashMap, actual["zzz"].class
    if 1.class == Integer
      # avoid deprecation warning as Ruby 2.4 unifies Fixnum and Bignum into Integer
      assert_equal Integer, actual["zzz"]["bar"].class
    else
      assert_equal Bignum, actual["zzz"]["bar"].class
    end
    assert_equal(-9, actual["zzz"]["bar"])
  end

  def test_can_parse_returning_ruby_objects_string_keys
    expected = {
      "a"=>"żółć", # string
      "b"=>true,    # boolean
      "c"=>12345,   # number
      "d"=>
       [true,
        [false,
         [-123456789, nil],
         0.39676E1,
         ["Something else.", false],
         nil]], # mix it up array
      "e"=>{"zero"=>nil, "one"=>1, "two"=>2, "three"=>[3], "four"=>[0, 1, 2, 3, 4]}, # hash
      "żółć"=>nil,# nil
      "h"=>{"a"=>{"b"=>{"c"=>{"d"=>{"e"=>{"f"=>{"g"=>nil}}}}}}},# deep hash, not that deep
      "i"=>[[[[[[[nil]]]]]]] # deep array, again, not that deep
    }
    json = JrJackson::Json.dump(expected)
    actual = JrJackson::Json.parse_ruby(json)
    assert_equal expected, actual
    actual = JrJackson::Ruby.parse(json, {})
    assert_equal expected, actual
  end

  def test_can_parse_returning_ruby_objects_symbol_keys
    expected = {:a=>"Alpha",
     :b=>true,
     :c=>12345,
     :d=>
      [true, [false, [-123456789, nil], 3.9676, ["Something else.", false], nil]],
     :e=>{:zero=>nil, :one=>1, :two=>2, :three=>[3], :four=>[0, 1, 2, 3, 4]},
     :f=>nil,
     :h=>{:a=>{:b=>{:c=>{:d=>{:e=>{:f=>{:g=>nil}}}}}}},
     :i=>[[[[[[[nil]]]]]]]
    }
    json = JrJackson::Json.dump(expected)
    actual = JrJackson::Ruby.parse_sym(json, {})
    assert_equal expected, actual
  end

  def test_can_parse_nulls
    expected = {"foo" => nil}
    json = '{"foo":null}'
    actual = JrJackson::Json.parse(json)
    assert_equal expected, actual
  end

  def test_stringio
    expected = {"foo" => 5, "utf8" => "żółć"}
    json = ::StringIO.new('{"foo":5, "utf8":"żółć"}')
    actual = JrJackson::Json.load_ruby(json)
    assert_equal expected, actual
  end

  def test_ruby_io
    expected = {"foo" => 5, "bar" => 6, "utf8" => "żółć"}
    json, w = IO.pipe
    w.write('{"foo":5, "bar":6, "utf8":"żółć"}')
    w.close
    actual = JrJackson::Json.load(json)
    assert_equal expected, actual
  end

  def test_bad_utf
    assert_raise JrJackson::ParseError do
      JrJackson::Json.load("\x82\xAC\xEF")
    end
  end

  def test_can_parse_bignum
    expected = 12345678901234567890123456789
    json = '{"foo":12345678901234567890123456789}'

    actual = JrJackson::Json.parse(json)['foo']
    assert_equal expected, actual
  end

  def test_can_parse_big_decimals
    expected = BigDecimal('0.12345678901234567890123456789')
    json = '{"foo":0.12345678901234567890123456789}'

    actual = JrJackson::Json.parse(json, :use_bigdecimal => true)['foo']
    assert_bigdecimal_equal expected, actual

    actual = JrJackson::Json.parse(json, :use_bigdecimal => true, :symbolize_keys => true)[:foo]
    assert_bigdecimal_equal expected, actual

    actual = JrJackson::Java.parse(json, {})['foo']
    assert_bigdecimal_similar expected, actual

    actual = JrJackson::Json.parse(json, :use_bigdecimal => true, :raw => true)['foo']
    assert_bigdecimal_similar expected, actual

  end

  def test_can_serialize_object
    obj = Object.new
    actual = JrJackson::Json.dump({"foo" => obj})
    assert_equal "{\"foo\":\"#{obj}\"}", actual
  end

  def test_can_serialize_basic_object
    obj = BasicObject.new
    actual = JrJackson::Json.dump({"foo" => obj})
    assert_equal "{\"foo\":\"#<BasicObject>\"}", actual
  end

  def test_can_serialize_basic_object_subclass
    obj = Test::Unit::NullObj.new
    actual = JrJackson::Json.dump({"foo" => obj})
    assert_equal "{\"foo\":\"#<Test::Unit::NullObj>\"}", actual
  end

  def test_can_serialize_custom_object
    obj = Test::Unit::CustomObj.new
    actual = JrJackson::Json.dump({"foo" => obj})
    assert_equal "{\"foo\":\"#{obj}\"}", actual
  end

  def test_supports_pretty_printing
    object = {"foo" => 5, "utf8" => "żółć"}
    actual = JrJackson::Json.dump(object, :pretty => true)
    assert_equal "{\n  \"foo\" : 5,\n  \"utf8\" : \"żółć\"\n}", actual
  end

  def test_can_serialise_non_string_keys
    object = {5 => "foo"}
    actual = JrJackson::Json.dump(object)
    assert_equal "{\"5\":\"foo\"}", actual
  end

  def test_can_serialise_regex_match_data
    re = %r|(foo)(bar)(baz)|
    m = re.match('foobarbaz')
    object = {'matched' => m[2]}
    actual = JrJackson::Json.dump(object)
    assert_equal "{\"matched\":\"bar\"}", actual
  end

  def test_can_mix_java_and_ruby_objects
    json = '{"utf8":"żółć", "moo": "bar", "arr": [2,3,4], "flo": 3.33}'
    timeobj = Time.new(2015,11,11,11,11,11,0).utc
    expected = '{"mixed":{"arr":[2,3,4],"utf8":"żółć","flo":3.33,"zzz":{"one":1.0,"two":2,"six":6.0},"moo":"bar"},"time":"2015-11-11 11:11:11 +0000"}'
    object = JrJackson::Json.parse_java(json)
    object["zzz"] = CustomToJson.new(1.0, 2, 6.0)
    mixed = {"mixed" => object}
    mixed['time'] = timeobj
    actual = JrJackson::Json.dump(mixed)
    assert_equal expected, actual
  end

  def test_can_serialize_exceptions
    e = StrangeError.new("Something immensely bad happened")
    object = {'error' => e}
    actual = JrJackson::Json.dump(object)
    assert_equal "{\"error\":\"#{e.inspect}\"}", actual
  end

  def test_can_serialize_class
    object = {"foo" => BasicObject}
    actual = JrJackson::Json.dump(object)
    assert_equal "{\"foo\":\"#{BasicObject.inspect}\"}", actual
  end

  def test_can_handle_big_numbers
    object = {"foo" => 2**63, "bar" => 65536}
    actual = JrJackson::Json.dump(object)
    assert_equal "{\"foo\":9223372036854775808,\"bar\":65536}", actual
  end


  # This test failed more often than not before fixing the underlying code
  # and would fail every time if `100_000.times` was changed to `loop`
  def test_concurrent_dump
    now = Time.now
    num_threads = 100

    threads = num_threads.times.map do |i|
      Thread.new do
        100_000.times { JrJackson::Json.dump("a" => now) }
      end
    end
    threads.each(&:join)
  end

  # -----------------------------

  def assert_bigdecimal_equal(expected, actual)
    assert_equal expected, actual
    assert_equal expected.class, actual.class
    assert_equal BigDecimal, actual.class
  end

  def assert_bigdecimal_similar(expected, actual)
    assert_equal BigDecimal(expected.to_s).round(11), BigDecimal(actual.to_s).round(11)
    assert_equal Java::JavaMath::BigDecimal, actual.class
  end
end
