package org.jruby.puma;

import org.jruby.*;
import org.jruby.anno.JRubyMethod;
import org.jruby.runtime.ObjectAllocator;
import org.jruby.runtime.ThreadContext;
import org.jruby.runtime.builtin.IRubyObject;
import org.jruby.util.ByteList;

/**
 * @author kares
 */
public class IOBuffer extends RubyObject {

    private static final ObjectAllocator ALLOCATOR = new ObjectAllocator() {
        public IRubyObject allocate(Ruby runtime, RubyClass klass) {
            return new IOBuffer(runtime, klass);
        }
    };

    public static void createIOBuffer(Ruby runtime) {
        RubyModule mPuma = runtime.defineModule("Puma");
        RubyClass cIOBuffer = mPuma.defineClassUnder("IOBuffer", runtime.getObject(), ALLOCATOR);
        cIOBuffer.defineAnnotatedMethods(IOBuffer.class);
    }

    private static final int DEFAULT_SIZE = 4096;

    final ByteList buffer = new ByteList(DEFAULT_SIZE);

    IOBuffer(Ruby runtime, RubyClass klass) {
        super(runtime, klass);
    }

    @JRubyMethod
    public RubyInteger used(ThreadContext context) {
        return context.runtime.newFixnum(buffer.getRealSize());
    }

    @JRubyMethod
    public RubyInteger capacity(ThreadContext context) {
        return context.runtime.newFixnum(buffer.unsafeBytes().length);
    }

    @JRubyMethod
    public IRubyObject reset() {
        buffer.setRealSize(0);
        return this;
    }

    @JRubyMethod(name = { "to_s", "to_str" })
    public RubyString to_s(ThreadContext context) {
        return RubyString.newStringShared(context.runtime, buffer.unsafeBytes(), 0, buffer.getRealSize());
    }

    @JRubyMethod(name = "<<")
    public IRubyObject add(IRubyObject str) {
        addImpl(str.convertToString());
        return this;
    }

    @JRubyMethod(rest = true)
    public IRubyObject append(IRubyObject[] strs) {
        for (IRubyObject str : strs) addImpl(str.convertToString());
        return this;
    }

    private void addImpl(RubyString str) {
        buffer.append(str.getByteList());
    }

}
