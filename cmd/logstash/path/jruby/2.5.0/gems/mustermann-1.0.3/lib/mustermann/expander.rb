# frozen_string_literal: true
require 'mustermann/ast/expander'
require 'mustermann/caster'
require 'mustermann'

module Mustermann
  # Allows fine-grained control over pattern expansion.
  #
  # @example
  #   expander = Mustermann::Expander.new(additional_values: :append)
  #   expander << "/users/:user_id"
  #   expander << "/pages/:page_id"
  #
  #   expander.expand(page_id: 58, format: :html5) # => "/pages/58?format=html5"
  class Expander
    attr_reader :patterns, :additional_values, :caster

    # @param [Array<#to_str, Mustermann::Pattern>] patterns list of patterns to expand, see {#add}.
    # @param [Symbol] additional_values behavior when encountering additional values, see {#expand}.
    # @param [Hash] options used when creating/expanding patterns, see {Mustermann.new}.
    def initialize(*patterns, additional_values: :raise, **options, &block)
      unless additional_values == :raise or additional_values == :ignore or additional_values == :append
        raise ArgumentError, "Illegal value %p for additional_values" % additional_values
      end

      @patterns          = []
      @api_expander      = AST::Expander.new
      @additional_values = additional_values
      @options           = options
      @caster            = Caster.new
      add(*patterns, &block)
    end

    # Add patterns to expand.
    #
    # @example
    #   expander = Mustermann::Expander.new
    #   expander.add("/:a.jpg", "/:b.png")
    #   expander.expand(a: "pony") # => "/pony.jpg"
    #
    # @param [Array<#to_str, Mustermann::Pattern>] patterns list of to add for expansion, Strings will be compiled to patterns.
    # @return [Mustermann::Expander] the expander
    def add(*patterns)
      patterns.each do |pattern|
        pattern = Mustermann.new(pattern, **@options)
        if block_given?
          @api_expander.add(yield(pattern))
        else
          raise NotImplementedError, "expanding not supported for #{pattern.class}" unless pattern.respond_to? :to_ast
          @api_expander.add(pattern.to_ast)
        end
        @patterns << pattern
      end
      self
    end

    alias_method :<<, :add

    # Register a block as simple hash transformation that runs before expanding the pattern.
    # @return [Mustermann::Expander] the expander
    #
    # @overload cast
    #   Register a block as simple hash transformation that runs before expanding the pattern for all entries.
    #
    #   @example casting everything that implements to_param to param
    #     expander.cast { |o| o.to_param if o.respond_to? :to_param }
    #
    #   @yield every key/value pair
    #   @yieldparam key [Symbol] omitted if block takes less than 2
    #   @yieldparam value [Object] omitted if block takes no arguments
    #   @yieldreturn [Hash{Symbol: Object}] will replace key/value pair with returned hash
    #   @yieldreturn [nil, false] will keep key/value pair in hash
    #   @yieldreturn [Object] will replace value with returned object
    #
    # @overload cast(*type_matchers)
    #   Register a block as simple hash transformation that runs before expanding the pattern for certain entries.
    #
    #   @example convert user to user_id
    #     expander = Mustermann::Expander.new('/users/:user_id')
    #     expand.cast(:user) { |user| { user_id: user.id } }
    #
    #     expand.expand(user: User.current) # => "/users/42"
    #
    #   @example convert user, page, image to user_id, page_id, image_id
    #     expander = Mustermann::Expander.new('/users/:user_id', '/pages/:page_id', '/:image_id.jpg')
    #     expand.cast(:user, :page, :image) { |key, value| { "#{key}_id".to_sym => value.id } }
    #
    #     expand.expand(user: User.current) # => "/users/42"
    #
    #   @example casting to multiple key/value pairs
    #     expander = Mustermann::Expander.new('/users/:user_id/:image_id.:format')
    #     expander.cast(:image) { |i| { user_id: i.owner.id, image_id: i.id, format: i.format } }
    #
    #     expander.expander(image: User.current.avatar) # => "/users/42/avatar.jpg"
    #
    #   @example casting all ActiveRecord objects to param
    #     expander.cast(ActiveRecord::Base, &:to_param)
    #
    #   @param [Array<Symbol, Regexp, #===>] type_matchers
    #     To identify key/value pairs to match against.
    #     Regexps and Symbols match against key, everything else matches against value.
    #
    #   @yield every key/value pair
    #   @yieldparam key [Symbol] omitted if block takes less than 2
    #   @yieldparam value [Object] omitted if block takes no arguments
    #   @yieldreturn [Hash{Symbol: Object}] will replace key/value pair with returned hash
    #   @yieldreturn [nil, false] will keep key/value pair in hash
    #   @yieldreturn [Object] will replace value with returned object
    #
    # @overload cast(*cast_objects)
    #
    #   @param [Array<#cast>] cast_objects
    #     Before expanding, will call #cast on these objects for each key/value pair.
    #     Return value will be treated same as block return values described above.
    def cast(*types, &block)
      caster.register(*types, &block)
      self
    end

    # @example Expanding a pattern
    #   pattern = Mustermann::Expander.new('/:name', '/:name.:ext')
    #   pattern.expand(name: 'hello')             # => "/hello"
    #   pattern.expand(name: 'hello', ext: 'png') # => "/hello.png"
    #
    # @example Handling additional values
    #   pattern = Mustermann::Expander.new('/:name', '/:name.:ext')
    #   pattern.expand(:ignore, name: 'hello', ext: 'png', scale: '2x') # => "/hello.png"
    #   pattern.expand(:append, name: 'hello', ext: 'png', scale: '2x') # => "/hello.png?scale=2x"
    #   pattern.expand(:raise,  name: 'hello', ext: 'png', scale: '2x') # raises Mustermann::ExpandError
    #
    # @example Setting additional values behavior for the expander object
    #   pattern = Mustermann::Expander.new('/:name', '/:name.:ext', additional_values: :append)
    #   pattern.expand(name: 'hello', ext: 'png', scale: '2x') # => "/hello.png?scale=2x"
    #
    # @param [Symbol] behavior
    #   What to do with additional key/value pairs not present in the values hash.
    #   Possible options: :raise, :ignore, :append.
    #
    # @param [Hash{Symbol: #to_s, Array<#to_s>}] values
    #   Values to use for expansion.
    #
    # @return [String] expanded string
    # @raise [NotImplementedError] raised if expand is not supported.
    # @raise [Mustermann::ExpandError] raised if a value is missing or unknown
    def expand(behavior = nil, values = {})
      behavior, values = nil, behavior if behavior.is_a? Hash
      values = map_values(values)

      case behavior || additional_values
      when :raise  then @api_expander.expand(values)
      when :ignore then with_rest(values) { |uri, rest| uri }
      when :append then with_rest(values) { |uri, rest| append(uri, rest) }
      else raise ArgumentError, "unknown behavior %p" % behavior
      end
    end

    # @see Object#==
    def ==(other)
      return false unless other.class == self.class
      other.patterns == patterns and other.additional_values == additional_values
    end

    # @see Object#eql?
    def eql?(other)
      return false unless other.class == self.class
      other.patterns.eql? patterns and other.additional_values.eql? additional_values
    end

    # @see Object#hash
    def hash
      patterns.hash + additional_values.hash
    end

    def expandable?(values)
      return false unless values
      expandable, _ = split_values(map_values(values))
      @api_expander.expandable? expandable
    end

    def with_rest(values)
      expandable, non_expandable = split_values(values)
      yield expand(:raise, slice(values, expandable)), slice(values, non_expandable)
    end

    def split_values(values)
      expandable     = @api_expander.expandable_keys(values.keys)
      non_expandable = values.keys - expandable
      [expandable, non_expandable]
    end

    def slice(hash, keys)
      Hash[keys.map { |k| [k, hash[k]] }]
    end

    def append(uri, values)
      return uri unless values and values.any?
      entries = values.map { |pair| pair.map { |e| @api_expander.escape(e, also_escape: /[\/\?#\&\=%]/) }.join(?=) }
      "#{ uri }#{ uri[??]??&:?? }#{ entries.join(?&) }"
    end

    def map_values(values)
      values = values.dup
      @api_expander.keys.each { |key| values[key] ||= values.delete(key.to_s) if values.include? key.to_s }
      caster.cast(values).delete_if { |k, v| v.nil? }
    end

    private :with_rest, :slice, :append, :caster, :map_values, :split_values
  end
end
