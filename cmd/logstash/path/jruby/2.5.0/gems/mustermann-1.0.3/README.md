# The Amazing Mustermann

*Make sure you view the correct docs: [latest release](http://rubydoc.info/gems/mustermann/frames), [master](http://rubydoc.info/github/rkh/mustermann/master/frames).*

Welcome to [Mustermann](http://en.wikipedia.org/wiki/List_of_placeholder_names_by_language#German). Mustermann is your personal string matching expert. As an expert in the field of strings and patterns, Mustermann keeps its runtime dependencies to a minimum and is fully covered with specs and documentation.

Given a string pattern, Mustermann will turn it into an object that behaves like a regular expression and has comparable performance characteristics.

``` ruby
if '/foo/bar' =~ Mustermann.new('/foo/*')
  puts 'it works!'
end

case 'something.png'
when Mustermann.new('foo/*') then puts "prefixed with foo"
when Mustermann.new('*.pdf') then puts "it's a PDF"
when Mustermann.new('*.png') then puts "it's an image"
end

pattern = Mustermann.new('/:prefix/*.*')
pattern.params('/a/b.c') # => { "prefix" => "a", splat => ["b", "c"] }
```

## Overview

### Features

* **[Pattern Types](#-pattern-types):** Mustermann supports a wide variety of different pattern types, making it compatible with a large variety of existing software.
* **[Fine Grained Control](#-available-options):** You can easily adjust matching behavior and add constraints to the placeholders and capture groups.
* **[Binary Operators](#-binary-operators) and [Concatenation](#-concatenation):** Patterns can be combined into composite patterns using binary operators.
* **[Regexp Look Alike](#-regexp-look-alike):** Mustermann patterns can be used as a replacement for regular expressions.
* **[Parameter Parsing](#-parameter-parsing):** Mustermann can parse matched parameters into a Sinatra-style "params" hash, including type casting.
* **[Peeking](#-peeking):** Lets you check if the beginning of a string matches a pattern.
* **[Expanding](#-expanding):** Besides parsing a parameters from an input string, a pattern object can also be used to generate a string from a set of parameters.
* **[Generating Templates](#-generating-templates):** This comes in handy when wanting to hand on patterns rather than fully expanded strings as part of an external API.
* **[Proc Look Alike](#-proc-look-alike):** Pass on a pattern instead of a block.
* **[Duck Typing](#-duck-typing):** You can create your own pattern-like objects by implementing `to_pattern`.
* **[Performance](#-performance):** Patterns are implemented with both performance and a low memory footprint in mind.

### Additional Tooling

These features are included in the library, but not loaded by default

* **[Mapper](#-mapper):** A simple tool for mapping one string to another based on patterns.
* **[Sinatra Integration](#-sinatra-integration):** Mustermann can be used as a [Sinatra](http://www.sinatrarb.com/) extension. Sinatra 2.0 and beyond will use Mustermann by default.

<a name="-pattern-types"></a>
## Pattern Types

Mustermann support multiple pattern types. A pattern type defines the syntax, matching semantics and whether certain features, like [expanding](#-expanding) and [generating templates](#-generating-templates), are available.

You can create a pattern of a certain type by passing `type` option to `Mustermann.new`:

``` ruby
require 'mustermann'
pattern = Mustermann.new('/*/**', type: :shell)
```

Note that this will use the type as suggestion: When passing in a string argument, it will create a pattern of the given type, but it might choose a different type for other objects (a regular expression argument will always result in a [regexp](#-pattern-details-regexp) pattern, a symbol always in a [sinatra](#-pattern-details-sinatra) pattern, etc).

Alternatively, you can also load and instantiate the pattern type directly:

``` ruby
require 'mustermann/shell'
pattern = Mustermann::Shell.new('/*/**')
```

Mustermann itself includes the [sinatra](#-sinatra-pattern), [identity](#-identity-pattern) and [regexp](#-regexp-pattern) pattern types. Other pattern types are available as separate gems.

<a name="-binary-operators"></a>
## Binary Operators

Patterns can be combined via binary operators. These are:

* `|` (or):  Resulting pattern matches if at least one of the input pattern matches.
* `&` (and): Resulting pattern matches if all input patterns match.
* `^` (xor): Resulting pattern matches if exactly one of the input pattern matches.

``` ruby
require 'mustermann'

first  = Mustermann.new('/foo/:input')
second = Mustermann.new('/:input/bar')

first | second === "/foo/foo" # => true
first | second === "/foo/bar" # => true

first & second === "/foo/foo" # => false
first & second === "/foo/bar" # => true

first ^ second === "/foo/foo" # => true
first ^ second === "/foo/bar" # => false
```

These resulting objects are fully functional pattern objects, allowing you to call methods like `params` or `to_proc` on them. Moreover, *or* patterns created solely from expandable patterns will also be expandable. The same logic also applies to generating templates from *or* patterns.

<a name="-concatenation"></a>
## Concatenation

Similar to [Binary Operators](#-binary-operators), two patterns can be concatenated using `+`.

``` ruby
require 'mustermann'

prefix = Mustermann.new("/:prefix")
about  = prefix + "/about"

about.params("/main/about") # => {"prefix" => "main"}
```

Patterns of different types can be mixed. The availability of `to_templates` and `expand` depends on the patterns being concatenated.

<a name="-regexp-look-alike"></a>
## Regexp Look Alike

Pattern objects mimic Ruby's `Regexp` class by implementing `match`, `=~`, `===`, `names` and `named_captures`.

``` ruby
require 'mustermann'

pattern = Mustermann.new('/:page')
pattern.match('/')     # => nil
pattern.match('/home') # => #<MatchData "/home" page:"home">
pattern =~ '/home'     # => 0
pattern === '/home'    # => true (this allows using it in case statements)
pattern.names          # => ['page']
pattern.names          # => {"page"=>[1]}

pattern = Mustermann.new('/home', type: :identity)
pattern.match('/')     # => nil
pattern.match('/home') # => #<Mustermann::SimpleMatch "/home">
pattern =~ '/home'     # => 0
pattern === '/home'    # => true (this allows using it in case statements)
pattern.names          # => []
pattern.names          # => {}
```

Moreover, patterns based on regular expressions (all but `identity` and `shell`) automatically convert to regular expressions when needed:

``` ruby
require 'mustermann'

pattern = Mustermann.new('/:page')
union   = Regexp.union(pattern, /^$/)

union =~ "/foo" # => 0
union =~ ""     # => 0

Regexp.try_convert(pattern) # => /.../
```

This way, unless some code explicitly checks the class for a regular expression, you should be able to pass in a pattern object instead even if the code in question was not written with Mustermann in mind.

<a name="-parameter-parsing"></a>
## Parameter Parsing

Besides being a `Regexp` look-alike, Mustermann also adds a `params` method, that will give you a Sinatra-style hash:

``` ruby
require 'mustermann'

pattern = Mustermann.new('/:prefix/*.*')
pattern.params('/a/b.c') # => { "prefix" => "a", splat => ["b", "c"] }
```

For patterns with typed captures, it will also automatically convert them:

``` ruby
require 'mustermann'

pattern = Mustermann.new('/<prefix>/<int:id>', type: :flask)
pattern.params('/page/10') # => { "prefix" => "page", "id" => 10 }
```

<a name="-peeking"></a>
## Peeking

Peeking gives the option to match a pattern against the beginning of a string rather the full string. Patterns come with four methods for peeking:

* `peek` returns the matching substring.
* `peek_size` returns the number of characters matching.
* `peek_match` will return a `MatchData` or `Mustermann::SimpleMatch` (just like `match` does for the full string)
* `peek_params` will return the `params` hash parsed from the substring and the number of characters.

All of the above will turn `nil` if there was no match.

``` ruby
require 'mustermann'

pattern = Mustermann.new('/:prefix')
pattern.peek('/foo/bar')      # => '/foo'
pattern.peek_size('/foo/bar') # => 4

path_info    = '/foo/bar'
params, size = patter.peek_params(path_info)  # params == { "prefix" => "foo" }
rest         = path_info[size..-1]            # => "/bar"
```

<a name="-expanding"></a>
## Expanding

Similarly to parsing, it is also possible to generate a string from a pattern by expanding it with a hash.
For simple expansions, you can use `Pattern#expand`.

``` ruby
pattern = Mustermann.new('/:file(.:ext)?')
pattern.expand(file: 'pony')             # => "/pony"
pattern.expand(file: 'pony', ext: 'jpg') # => "/pony.jpg"
pattern.expand(ext: 'jpg')               # raises Mustermann::ExpandError
```

Expanding can be useful for instance when implementing link helpers.

### Expander Objects

To get fine-grained control over expansion, you can use `Mustermann::Expander` directly.

You can create an expander object directly from a string:

``` ruby
require 'mustermann/expander'
expander = Mustermann::Expander("/:file.jpg")
expander.expand(file: 'pony') # => "/pony.jpg"

expander = Mustermann::Expander(":file(.:ext)", type: :rails)
expander.expand(file: 'pony', ext: 'jpg') # => "/pony.jpg"
```

Or you can pass it a pattern instance:

``` ruby
require 'mustermann'
pattern = Mustermann.new("/:file")

require 'mustermann/expander'
expander = Mustermann::Expander.new(pattern)
```

### Expanding Multiple Patterns

You can add patterns to an expander object via `<<`:

``` ruby
require 'mustermann'

expander = Mustermann::Expander.new
expander << "/users/:user_id"
expander << "/pages/:page_id"

expander.expand(user_id: 15) # => "/users/15"
expander.expand(page_id: 58) # => "/pages/58"
```

You can set pattern options when creating the expander:

``` ruby
require 'mustermann'

expander = Mustermann::Expander.new(type: :template)
expander << "/users/{user_id}"
expander << "/pages/{page_id}"
```

Additionally, it is possible to combine patterns of different types:

``` ruby
require 'mustermann'

expander = Mustermann::Expander.new
expander << Mustermann.new("/users/{user_id}", type: :template)
expander << Mustermann.new("/pages/:page_id",  type: :rails)
```

### Handling Additional Values

The handling of additional values passed in to `expand` can be changed by setting the `additional_values` option:

``` ruby
require 'mustermann'

expander = Mustermann::Expander.new("/:slug", additional_values: :raise)
expander.expand(slug: "foo", value: "bar") # raises Mustermann::ExpandError

expander = Mustermann::Expander.new("/:slug", additional_values: :ignore)
expander.expand(slug: "foo", value: "bar") # => "/foo"

expander = Mustermann::Expander.new("/:slug", additional_values: :append)
expander.expand(slug: "foo", value: "bar") # => "/foo?value=bar"
```

It is also possible to pass this directly to the `expand` call:

``` ruby
require 'mustermann'

pattern = Mustermann.new('/:slug')
pattern.expand(:append, slug: "foo", value: "bar") # => "/foo?value=bar"
```

<a name="-generating-templates"></a>
## Generating Templates

You can generate a list of URI templates that correspond to a Mustermann pattern (it is a list rather than a single template, as most pattern types are significantly more expressive than URI templates).

This comes in quite handy since URI templates are not made for pattern matching. That way you can easily use a more precise template syntax and have it automatically generate hypermedia links for you.

Template generation is supported by almost all patterns (notable exceptions are `shell`, `regexp` and `simple` patterns).

``` ruby
require 'mustermann'

Mustermann.new("/:name").to_templates                   # => ["/{name}"]
Mustermann.new("/:foo(@:bar)?/*baz").to_templates       # => ["/{foo}@{bar}/{+baz}", "/{foo}/{+baz}"]
Mustermann.new("/{name}", type: :template).to_templates # => ["/{name}"
```

Union Composite patterns (with the | operator) support template generation if all patterns they are composed of also support it.

``` ruby
require 'mustermann'

pattern  = Mustermann.new('/:name')
pattern |= Mustermann.new('/{name}', type: :template)
pattern |= Mustermann.new('/example/*nested')
pattern.to_templates # => ["/{name}", "/example/{+nested}"]
```

If accepting arbitrary patterns, you can and should use `respond_to?` to check feature availability.

``` ruby
if pattern.respond_to? :to_templates
  pattern.to_templates
else
  warn "does not support template generation"
end
```

<a name="-proc-look-alike"></a>
## Proc Look Alike

Patterns implement `to_proc`:

``` ruby
require 'mustermann'
pattern  = Mustermann.new('/foo')
callback = pattern.to_proc # => #<Proc>

callback.call('/foo') # => true
callback.call('/bar') # => false
```

They can therefore be easily passed to methods expecting a block:

``` ruby
require 'mustermann'

list    = ["foo", "example@email.com", "bar"]
pattern = Mustermann.new(":name@:domain.:tld")
email   = list.detect(&pattern) # => "example@email.com"
```

<a name="-mapper"></a>
## Mapper


You can use a mapper to transform strings according to two or more mappings:

``` ruby
require 'mustermann/mapper'

mapper = Mustermann::Mapper.new("/:page(.:format)?" => ["/:page/view.:format", "/:page/view.html"])
mapper['/foo']     # => "/foo/view.html"
mapper['/foo.xml'] # => "/foo/view.xml"
mapper['/foo/bar'] # => "/foo/bar"
```

<a name="-sinatra-integration"></a>
## Sinatra Integration

All patterns implement `match`, which means they can be dropped into Sinatra and other Rack routers:

``` ruby
require 'sinatra'
require 'mustermann'

get Mustermann.new('/:foo') do
  params[:foo]
end
```

In fact, since using this with Sinatra is the main use case, it comes with a build-in extension for **Sinatra 1.x**.

``` ruby
require 'sinatra'
require 'mustermann'

register Mustermann

# this will use Mustermann rather than the built-in pattern matching
get '/:slug(.ext)?' do
  params[:slug]
end
```

### Configuration

You can change what pattern type you want to use for your app via the `pattern` option:

``` ruby
require 'sinatra/base'
require 'mustermann'

class MyApp < Sinatra::Base
  register Mustermann
  set :pattern, type: :shell

  get '/images/*.png' do
    send_file request.path_info
  end

  get '/index{.htm,.html,}' do
    erb :index
  end
end
```

You can use the same setting for options:

``` ruby
require 'sinatra'
require 'mustermann'

register Mustermann
set :pattern, capture: { ext: %w[png jpg html txt] }

get '/:slug(.:ext)?' do
  # slug will be 'foo' for '/foo.png'
  # slug will be 'foo.bar' for '/foo.bar'
  # slug will be 'foo.bar' for '/foo.bar.html'
  params[:slug]
end
```

It is also possible to pass in options to a specific route:

``` ruby
require 'sinatra'
require 'mustermann'

register Mustermann

get '/:slug(.:ext)?', pattern: { greedy: false } do
  # slug will be 'foo' for '/foo.png'
  # slug will be 'foo' for '/foo.bar'
  # slug will be 'foo' for '/foo.bar.html'
  params[:slug]
end
```

Of course, all of the above can be combined.
Moreover, the `capture` and the `except` option can be passed to route directly.
And yes, this also works with `before` and `after` filters.

``` ruby
require 'sinatra/base'
require 'sinatra/respond_with'
require 'mustermann'

class MyApp < Sinatra::Base
  register Mustermann, Sinatra::RespondWith
  set :pattern, capture: { id: /\d+/ } # id will only match digits

  # only capture extensions known to Rack
  before '*:ext', capture: Rack::Mime::MIME_TYPES.keys do
    content_type params[:ext]                 # set Content-Type
    request.path_info = params[:splat].first  # drop the extension
  end

  get '/:id' do
    not_found unless page = Page.find params[:id]
    respond_with(page)
  end
end
```

### Why would I want this?

* It gives you fine grained control over the pattern matching
* Allows you to use different pattern styles in your app
* The default is more robust and powerful than the built-in patterns
* Sinatra 2.0 will use Mustermann internally
* Better exceptions for broken route syntax

### Why not include this in Sinatra 1.x?

* It would introduce breaking changes, even though these would be minor
* Like Sinatra 2.0, Mustermann requires Ruby 2.0 or newer

<a name="-duck-typing"></a>
## Duck Typing

<a name="-duck-typing-to-pattern"></a>
### `to_pattern`

All methods converting string input to pattern objects will also accept any arbitrary object that implements `to_pattern`:

``` ruby
require 'mustermann'

class MyObject
  def to_pattern(**options)
    Mustermann.new("/foo", **options)
  end
end

object = MyObject.new
Mustermann.new(object, type: :rails) # => #<Mustermann::Rails:"/foo">
```

It might also be that you want to call `to_pattern` yourself instead of `Mustermann.new`. You can load `mustermann/to_pattern` to implement this method for strings, regular expressions and pattern objects:

``` ruby
require 'mustermann/to_pattern'

"/foo".to_pattern               # => #<Mustermann::Sinatra:"/foo">
"/foo".to_pattern(type: :rails) # => #<Mustermann::Rails:"/foo">
%r{/foo}.to_pattern             # => #<Mustermann::Regular:"\\/foo">
"/foo".to_pattern.to_pattern    # => #<Mustermann::Sinatra:"/foo">
```

You can also use the `Mustermann::ToPattern` mixin to easily add `to_pattern` to your own objects:

``` ruby
require 'mustermann/to_pattern'

class MyObject
  include Mustermann::ToPattern

  def to_s
    "/foo"
  end
end

MyObject.new.to_pattern # => #<Mustermann::Sinatra:"/foo">
```

<a name="-duck-typing-respond-to"></a>
### `respond_to?`

You can and should use `respond_to?` to check if a pattern supports certain features.

``` ruby
require 'mustermann'
pattern = Mustermann.new("/")

puts "supports expanding"             if pattern.respond_to? :expand
puts "supports generating templates"  if pattern.respond_to? :to_templates
```

Alternatively, you can handle a `NotImplementedError` raised from such a method.

``` ruby
require 'mustermann'
pattern = Mustermann.new("/")

begin
  p pattern.to_templates
rescue NotImplementedError
  puts "does not support generating templates"
end
```

This behavior corresponds to what Ruby does, for instance for [`fork`](http://ruby-doc.org/core-2.1.1/NotImplementedError.html).

<a name="-available-options"></a>
## Available Options

<a name="-available-options--capture"></a>
### `capture`

Supported by: All types except `identity`, `shell` and `simple` patterns.

Most pattern types support changing the strings named captures will match via the `capture` options.

Possible values for a capture:

``` ruby
# String: Matches the given string (or any URI encoded version of it)
Mustermann.new('/index.:ext', capture: 'png')

# Regexp: Matches the Regular expression
Mustermann.new('/:id', capture: /\d+/)

# Symbol: Matches POSIX character class
Mustermann.new('/:id', capture: :digit)

# Array of the above: Matches anything in the array
Mustermann.new('/:id_or_slug', capture: [/\d+/, :word])

# Hash of the above: Looks up the hash entry by capture name and uses value for matching
Mustermann.new('/:id.:ext', capture: { id: /\d+/, ext: ['png', 'jpg'] })
```

Available POSIX character classes are: `:alnum`, `:alpha`, `:blank`, `:cntrl`, `:digit`, `:graph`, `:lower`, `:print`, `:punct`, `:space`, `:upper`, `:xdigit`, `:word` and `:ascii`.

<a name="-available-options--except"></a>
### `except`

Supported by: All types except `identity`, `shell` and `simple` patterns.

Given you supply a second pattern via the except option. Any string that would match the primary pattern but also matches the except pattern will not result in a successful match. Feel free to read that again. Or just take a look at this example:

``` ruby
pattern = Mustermann.new('/auth/*', except: '/auth/login')
pattern === '/auth/dunno' # => true
pattern === '/auth/login' # => false
```

Now, as said above, `except` treats the value as a pattern:

``` ruby
pattern = Mustermann.new('/*anything', type: :rails, except: '/*anything.png')
pattern === '/foo.jpg' # => true
pattern === '/foo.png' # => false
```

<a name="-available-options--greedy"></a>
### `greedy`

Supported by: All types except `identity` and `shell` patterns.
Default value: `true`

**Simple** patterns are greedy, meaning that for the pattern `:foo:bar?`, everything will be captured as `foo`, `bar` will always be `nil`. By setting `greedy` to `false`, `foo` will capture as little as possible (which in this case would only be the first letter), leaving the rest to `bar`.

**All other** supported patterns are semi-greedy. This means `:foo(.:bar)?` (`:foo(.:bar)` for Rails patterns) will capture everything before the *last* dot as `foo`. For these two pattern types, you can switch into non-greedy mode by setting the `greedy` option to false. In that case `foo` will only capture the part before the *first* dot.

Semi-greedy behavior is not specific to dots, it works with all characters or strings. For instance, `:a(foo:b)` will capture everything before the *last* `foo` as `a`, and `:foo(bar)?` will not capture a `bar` at the end.

``` ruby
pattern = Mustermann.new(':a.:b', greedy: true)
pattern.match('a.b.c.d') # => #<MatchData a:"a.b.c" b:"d">

pattern = Mustermann.new(':a.:b', greedy: false)
pattern.match('a.b.c.d') # => #<MatchData a:"a" b:"b.c.d">
```

<a name="-available-options--space_matches_plus"></a>
### `space_matches_plus`

Supported by: All types except `identity`, `regexp` and `shell` patterns.
Default value: `true`

Most pattern types will by default also match a plus sign for a space in the pattern:

``` ruby
Mustermann.new('a b') === 'a+b' # => true
```

You can disable this behavior via `space_matches_plus`:

``` ruby
Mustermann.new('a b', space_matches_plus: false) === 'a+b' # => false
```

**Important:** This setting has no effect on captures, captures will always keep plus signs as plus sings and spaces as spaces:

``` ruby
pattern = Mustermann.new(':x')
pattern.match('a b')[:x] # => 'a b'
pattern.match('a+b')[:x] # => 'a+b'
````

<a name="-available-options--uri_decode"></a>
### `uri_decode`

Supported by all pattern types.
Default value: `true`

Usually, characters in the pattern will also match the URI encoded version of these characters:

``` ruby
Mustermann.new('a b') === 'a b'   # => true
Mustermann.new('a b') === 'a%20b' # => true
```

You can avoid this by setting `uri_decode` to `false`:

``` ruby
Mustermann.new('a b', uri_decode: false) === 'a b'   # => true
Mustermann.new('a b', uri_decode: false) === 'a%20b' # => false
```

<a name="-available-options--ignore_unknown_options"></a>
### `ignore_unknown_options`

Supported by all patterns.
Default value: `false`

If you pass an option in that is not supported by the specific pattern type, Mustermann will raise an `ArgumentError`.
By setting `ignore_unknown_options` to `true`, it will happily ignore the option.

<a name="-performance"></a>
## Performance

It's generally a good idea to reuse pattern objects, since as much computation as possible is happening during object creation, so that the actual matching or expanding is quite fast.

Pattern objects should be treated as immutable. Their internals have been designed for both performance and low memory usage. To reduce pattern compilation, `Mustermann.new` and `Mustermann::Pattern.new` might return the same instance when given the same arguments, if that instance has not yet been garbage collected. However, this is not guaranteed, so do not rely on object identity.

### String Matching

When using a pattern instead of a regular expression for string matching, performance will usually be comparable.

In certain cases, Mustermann might outperform naive, equivalent regular expressions. It achieves this by using look-ahead and atomic groups in ways that work well with a backtracking, NFA-based regular expression engine (such as the Oniguruma/Onigmo engine used by Ruby). It can be difficult and error prone to construct complex regular expressions using these techniques by hand. This only applies to patterns generating an AST internally (all but [identity](#-pattern-details-identity), [shell](#-pattern-details-shell), [simple](#-pattern-details-simple) and [regexp](#-pattern-details-regexp) patterns).

When using a Mustermann pattern as a direct Regexp replacement (ie, via methods like `=~`, `match` or `===`), the overhead will be a single method dispatch, which some Ruby implementations might even eliminate with method inlining. This only applies to patterns using a regular expression internally (all but [identity](#-pattern-details-identity) and [shell](#-pattern-details-shell) patterns).

### Expanding

Pattern expansion significantly outperforms other, widely used Ruby tools for generating URLs from URL patterns in most use cases.

This comes with a few trade-offs:

* As with pattern compilation, as much computation as possible has been shifted to compiling expansion rules. This will add compilation overhead, which is why patterns only generate these rules on the first invocation to `Mustermann::Pattern#expand`. Create a `Mustermann::Expander` instance yourself to get better control over the point in time this computation should happen.
* Memory is sacrificed in favor of performance: The size of the expander object will grow linear with the number of possible combination for expansion keys ("/:foo/:bar" has one such combination, but "/(:foo/)?:bar?" has four)
* Parsing a params hash from a string generated from another params hash might not result in two identical hashes, and vice versa. Specifically, expanding ignores capture constraints, type casting and greediness.
* Partial expansion is (currently) not supported.

## Details on Pattern Types

<a name="-identity-pattern"></a>
### `identity`

**Supported options:**
[`uri_decode`](#-available-options--uri_decode),
[`ignore_unknown_options`](#-available-options--ignore_unknown_options).

<table>
  <thead>
    <tr>
      <th>Syntax Element</th>
      <th>Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><i>any character</i></td>
      <td>Matches exactly that character or a URI escaped version of it.</td>
    </tr>
  </tbody>
</table>

<a name="-regexp-pattern"></a>
### `regexp`

**Supported options:**
[`uri_decode`](#-available-options--uri_decode),
[`ignore_unknown_options`](#-available-options--ignore_unknown_options), `check_anchors`.

The pattern string (or actual Regexp instance) should not contain anchors (`^` outside of square brackets, `$`, `\A`, `\z`, or `\Z`).
Anchors will be injected where necessary by Mustermann.

By default, Mustermann will raise a `Mustermann::CompileError` if an anchor is encountered.
If you still want it to contain anchors at your own risk, set the `check_anchors` option to `false`.

Using anchors will break [peeking](#-peeking) and [concatenation](#-concatenation).

<table>
  <thead>
    <tr>
      <th>Syntax Element</th>
      <th>Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><i>any string</i></td>
      <td>Interpreted as regular expression.</td>
    </tr>
  </tbody>
</table>

<a name="-sinatra-pattern"></a>
### `sinatra`

**Supported options:**
[`capture`](#-available-options--capture),
[`except`](#-available-options--except),
[`greedy`](#-available-options--greedy),
[`space_matches_plus`](#-available-options--space_matches_plus),
[`uri_decode`](#-available-options--uri_decode),
[`ignore_unknown_options`](#-available-options--ignore_unknown_options).

<table>
  <thead>
    <tr>
      <th>Syntax Element</th>
      <th>Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><b>:</b><i>name</i> <i><b>or</b></i> <b>&#123;</b><i>name</i><b>&#125;</b></td>
      <td>
        Captures anything but a forward slash in a semi-greedy fashion. Capture is named <i>name</i>.
        Capture behavior can be modified with <tt>capture</tt> and <tt>greedy</tt> option.
      </td>
    </tr>
    <tr>
      <td><b>*</b><i>name</i> <i><b>or</b></i> <b>&#123;+</b><i>name</i><b>&#125;</b></td>
      <td>
        Captures anything in a non-greedy fashion. Capture is named <i>name</i>.
      </td>
    </tr>
    <tr>
      <td><b>*</b> <i><b>or</b></i> <b>&#123;+splat&#125;</b></td>
      <td>
        Captures anything in a non-greedy fashion. Capture is named splat.
        It is always an array of captures, as you can use it more than once in a pattern.
      </td>
    </tr>
    <tr>
      <td><b>(</b><i>expression</i><b>)</b></td>
      <td>
        Enclosed <i>expression</i> is a group. Useful when combined with <tt>?</tt> to make it optional,
        or to separate two elements that would otherwise be parsed as one.
      </td>
    </tr>
    <tr>
      <td><i>expression</i><b>|</b><i>expression</i><b>|</b><i>...</i></td>
      <td>
        Will match anything matching the nested expressions. May contain any other syntax element, including captures.
      </td>
    </tr>
    <tr>
      <td><i>x</i><b>?</b></td>
      <td>Makes <i>x</i> optional. For instance, <tt>(foo)?</tt> matches <tt>foo</tt> or an empty string.</td>
    </tr>
    <tr>
      <td><b>/</b></td>
      <td>
        Matches forward slash. Does not match URI encoded version of forward slash.
      </td>
    </tr>
    <tr>
      <td><b>\</b><i>x</i></td>
      <td>Matches <i>x</i> or URI encoded version of <i>x</i>. For instance <tt>\*</tt> matches <tt>*</tt>.</td>
    </tr>
    <tr>
      <td><i>any other character</i></td>
      <td>Matches exactly that character or a URI encoded version of it.</td>
    </tr>
  </tbody>
</table>
