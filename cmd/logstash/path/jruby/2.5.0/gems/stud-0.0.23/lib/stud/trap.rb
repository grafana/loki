module Stud
  # Bind a block to be called when a certain signal is received.
  #
  # Same arguments to Signal::trap.
  #
  # The behavior of this method is different than Signal::trap because
  # multiple handlers can request notification for the same signal.
  #
  # For example, this is valid:
  #
  #     Stud.trap("INT") { puts "Hello" }
  #     Stud.trap("INT") { puts "World" }
  #
  # When SIGINT is received, both callbacks will be invoked, in order.
  #
  # This helps avoid the situation where a library traps a signal outside of
  # your control.
  #
  # If something has already used Signal::trap, that callback will be saved
  # and scheduled the same way as any other Stud::trap.
  def self.trap(signal, &block)
    @traps ||= Hash.new { |h,k| h[k] = [] }

    if !@traps.include?(signal)
      # First trap call for this signal, tell ruby to invoke us.
      previous_trap = Signal::trap(signal) { simulate_signal(signal) }
      # If there was a previous trap (via Kernel#trap) set, make sure we remember it.
      if previous_trap.is_a?(Proc)
        # MRI's default traps are "DEFAULT" string
        # JRuby's default traps are Procs with a source_location of "(internal")
        if RUBY_ENGINE != "jruby" || previous_trap.source_location.first != "(internal)"
          @traps[signal] << previous_trap
        end
      end
    end

    @traps[signal] << block

    return block.object_id
  end # def self.trap

  # Simulate a signal. This lets you force an interrupt without
  # sending a signal to yourself.
  def self.simulate_signal(signal)
    #puts "Simulate: #{signal} w/ #{@traps[signal].count} callbacks"
    @traps[signal].each(&:call)
  end # def self.simulate_signal

  # Remove a previously set signal trap.
  #
  # 'signal' is the name of the signal ("INT", etc)
  # 'id' is the value returned by a previous Stud.trap() call
  def self.untrap(signal, id)
    @traps[signal].delete_if { |block| block.object_id == id }

    # Restore the default handler if there are no custom traps anymore.
    if @traps[signal].empty?
      @traps.delete(signal)
      Signal::trap(signal, "DEFAULT")
    end
  end # def self.untrap
end # module Stud

# Monkey-patch the main 'trap' stuff? This could be useful.
#module Signal
  #def trap(signal, value=nil, &block)
    #if value.nil?
      #Stud.trap(signal, &block)
    #else
      ## do nothing?
    #end
  #end # def trap
#end
#
#module Kernel
  #def trap(signal, value=nil, &block)
    #Signal.trap(signal, value, &block)
  #end
#end

