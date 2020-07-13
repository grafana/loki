v0.4.11
  Bump Jackson to v2.9.10, and jackson-databind to v2.9.10.1

v0.4.10
  fix concurrency issue when serializing dates. 
  Cache UTC TimeZone class to avoid unnecessary calls to synchronized method
  Use a ThreadLocal to hold per-thread instances of SimpleDateFormat to avoid
   unnecessary expensive clonings of that object
  Replace unsafe call to setDateFormat on static ObjectMapper class by creating
   an amended SerializationConfig

v0.4.9
  bump Jackson to v2.9.9, and jackson-databind to v2.9.9.3

v0.4.8
  fix serialisation of big numbers as Ruby 2.4 unifies Fixnum and Bignum into Integer 

v0.4.4
  fix for issue 64
    Ruby parse() null/nil handling

v0.4.3
  bump Jackson to v2.9.1
  make static_mapper public
  
v0.4.1
  fix for issue 55
    Refactor AnySerializer acceptable class detection that does not use an exception

v0.4.0
  Implement `to_s` and Exception serialization

v0.3.9
  Thanks to mkristian, now uses jar_dependencies
    Upgrade to Jackson v2.7.1
  fix for issue 51
    dont create "new RubyJacksonModule()" when creating a Provider
  fix for reports of symbol table overflow in Jackson when using JrJackson in very long running daemons (Logstash)
    disable the FAIL_ON_SYMBOL_HASH_OVERFLOW setting

v0.3.8
  fix for issue 47
    Update Jackson to v 2.6.3
  Change error message to better report failure in issue 46

v0.3.7
  fix for issue 46
    Add references to RubyAnySerializer so Jackson can
    use it for Ruby objects nested in Java objects

v0.3.6
  fix for issue 45
    use bytelist.begin instead of 0 generating from RubyString

v0.3.5
  give highest precedence to the to_json_data method

v0.3.4
  fix multi_json bug
    not serializing non-string keys

v0.3.3
  fix bigdecimal bug
    add require bigdecimal

v0.3.2
  update changelog

v0.3.1
  remove old jar

v0.3.0
  this is a major refactor.
  parse and generate performance improvements.
    see JrJackson::Ruby and JrJackson::Java modules
  pretty generation support.
  jruby 9.0.1.0 and 1.7.22
  jackson 2.6.1

v0.2.9
  fix for issue 39
    incorrect error when serializing BasicObject

v0.2.8
  fixes for issues-28,29,31
    correction for Time#to_s
    new options to control date serialization
    optimizations suggested by @headius
  jar compiled for jruby 1.7.17
  jruby 1.7.17
  jackson 2.4.4

v0.2.7
  fixes for issues-23,24
    add to_time as option for serializing Time like objects
  jar compiled for jruby 1.7.11

v0.2.6
  fix issue-20
    allow jruby to convert Ruby StringIO into Java
    by not type checking passed arg
    this is because jruby 1.7.9 has changed the type of java object backing Ruby StringIO
  jar compiled for jruby 1.7.8 (jruby 1.7.9 in the maven repo has an error in the pom.xml)
  jruby 1.7.8, jruby 1.7.9 (tested)
  jackson 2.3.0

v0.2.5
  fix issue-16
    reduce the gem size by:
      change pom.xml to only include relevant java jars
      exclude benchmaking from the gemspec files
  jruby 1.7.5
  jackson 2.2.3

v0.2.4
  fix issue-15
    return Ruby nil instead of Java null
  fix issue-14
    remove all usage of Ruby.getGlobalRuntime
    pass the runtime from the calling Ruby ThreadContext into the deserializers and converters
  jruby 1.7.5
  jackson 2.2.3

v0.2.3
  fix issue-12
    improve the serialization support for non Json Datatype Ruby objects
    now has support for serializing via toJava, to_h, to_hash, to_a, to_json
  fix for failing MultiJson unicode test
  jruby 1.7.4
  jackson 2.2.3

v0.2.2
  fix issue-13
    compile Java for 1.6 compatibility
  documentation tweaks
  jruby 1.7.4
  jackson 2.2.3

v0.2.1
  documentation tweaks
  fix issue-7
    add pluggable String and Symbol Converters for JSON values
  jruby 1.7.4
  jackson 2.2.3

v0.2.0
  extract all Java -> Ruby generation to reusable RubyUtils static class
  support BigDecimal
  remove JSON Api
  fixes issues 5, 6, 8,

  jruby 1.7.3
  jackson 2.2.2

v0.1.1
  fix Time regex
v0.1.0
  MutiJson compatibility
  switch to using almost all Java, i.e. define most of the ruby modules in Java
  jruby 1.7.3
  jackson 2.1.4
v0.0.7
  first release - minimal jruby wrapper around jackson 1.9.5 jars
