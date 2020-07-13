

LICENSE applicable to this library:

Apache License 2.0 see http://www.apache.org/licenses/LICENSE-2.0

### JrJackson:

a jruby library wrapping the JAVA Jackson jars`

__NOTE:__ Smile support has been temporarily dropped

The code has been refactored to use almost all Java.

There is now a MultiJson adapter added for JrJackson.

This release is compatible with JRuby 9.0.0.0 and higher.

***

#### NEWS

07 October 2015 - during serialisation, check and execute `to_json_data` method
first.

13th September 2015 - added support for a `to_json_data` method lookup.
Use this if you want to provide a JSON native data structure that best
represents your custom object.

11th May 2014 - Added to_time method call for Ruby object serialization

26th October 2013 - Added support to serialize arbitary (non JSON datatypes)
ruby objects.  Normally the toJava internal method is called, but additionally
to_h, to_hash, to_a and finally to_json are tried.  Be aware that the to_json
method might invoke a new selialization session (i.e. it may use the JSON gem)
and impact performance.

***

#### API

```
JrJackson::Json.load(string, options) -> hash like object
      aliased as parse
```
By default the load method will return Ruby objects (Hashes have string keys).
The options hash respects three symbol keys

+ :symbolize_keys

  Will return symbol keys in hashes

+ :raw

  Will return JRuby wrapped java objects that quack like ruby objects
  This is the fastest option but not by much

+ :use_bigdecimal

  Will return BigDecimal objects instead of Float.
  If used with the :raw option you will get Java::JavaMath::BigDecimal objects
  otherwise they are Ruby BigDecimal

+ :use_smallint

  Will return Integers objects instead of BigInteger

```
JrJackson::Json.dump(obj) -> json string
      aliased as generate
```
The dump method expects that the values of hashes or arrays are JSON data types,
the only exception to this is Ruby Symbol as values, they are converted to java strings
during serialization. __NOTE:__ All other objects should be converted to JSON data types before
serialization. See the wiki for more on this.

***

#### Internals

There are four Ruby sub modules of the JrJackson module

```JrJackson::Json```, this is the general external facade used by MultiJson.

```JrJackson::Raw```, this exists for backward compatibility, do not use this for new code.

```JrJackson::Ruby```, this is used by the external facade, you should use this directly. It returns Ruby objects e.g. Hash, Array, Symbol, String, Integer, BigDecimal etc.

```JrJackson::Java```, this is used by the external facade, you should use this directly. It returns Java objects e.g. ArrayList, HashMap, BigDecimal, BigInteger, Long, Float and String, JRuby wraps (mostly) them in a JavaProxy Ruby object.

***

#### Benchmarks

Credit to Chuck Remes for the benchmark and initial
investigation when the jruby, json gem and the jackson
libraries were young.

I compared Json (java) 1.8.3, Gson 0.6.1 and jackson 2.6.1 on jruby 1.7.22 and Oracle Java HotSpot(TM) 64-Bit Server VM 1.8.0_60-b27 +jit [linux-amd64]
All the benchmarks were run separately. A 727.9KB string of random json data is read from a file and handled 250 times, thereby attempting to balance invocation and parsing benchmarking.

```
generation/serialize

                                               user     system      total         real


json java generate: 250                        6.780      0.620      7.400    (  6.599)
gson generate: 250                             4.480      0.530      5.010    (  4.688)
jackson generate: 250                          2.200      0.010      2.210    (  2.128)

json mri parse: 250                            9.620      0.000      9.620    (  9.631)
oj mri parse: 250                              9.190      0.000      9.190    (  9.199)
json mri generate: 250                         8.400      0.010      8.410    (  8.419)
oj mri generate: 250                           6.980      0.010      6.990    (  6.999)


parsing/deserialize - after jrjackson parsing profiling

                                               user     system      total         real
json java parse: 250                           9.320      0.580      9.900    (  9.467)
jackson parse string keys: 250                 3.600      0.520      4.120    (  3.823)
jackson parse string + bigdecimal: 250         3.390      0.640      4.030    (  3.721)
jackson parse ruby compat: 250                 3.700      0.030      3.730    (  3.516)
jackson parse ruby ootb: 250                   3.490      0.120      3.610    (  3.420)
jackson parse sj: 250                          3.290      0.030      3.320    (  3.065)
jackson parse symbol keys: 250                 3.050      0.120      3.170    (  2.943)
jackson parse symbol + bigdecimal: 250         2.770      0.020      2.790    (  2.669)
jackson parse java + bigdecimal: 250           1.880      0.430      2.310    (  2.239)
jackson parse java + bigdecimal direct: 250    1.950      0.440      2.390    (  2.290)
jackson parse java ootb: 250                   1.990      0.030      2.020    (  1.925)
jackson parse java sym bd: 250                 1.920      0.000      1.920    (  1.898)
```

I have done IPS style benchmarks too.

Generation
```
jrjackson:   74539.4 i/s
    gson:    58288.3 i/s - 1.28x slower
    JSON:    54597.2 i/s - 1.37x slower
```

Parsing returning Ruby
```
symbol keys
jrjackson:    95203.9 i/s
     JSON:    40712.0 i/s - 2.34x slower

```
