package com.concurrent_ruby.ext;

import java.io.IOException;
import java.util.concurrent.Semaphore;
import org.jruby.Ruby;
import org.jruby.RubyClass;
import org.jruby.RubyFixnum;
import org.jruby.RubyModule;
import org.jruby.RubyNumeric;
import org.jruby.RubyObject;
import org.jruby.anno.JRubyClass;
import org.jruby.anno.JRubyMethod;
import org.jruby.runtime.ObjectAllocator;
import org.jruby.runtime.ThreadContext;
import org.jruby.runtime.builtin.IRubyObject;

public class JavaSemaphoreLibrary {

    public void load(Ruby runtime, boolean wrap) throws IOException {
        RubyModule concurrentMod = runtime.defineModule("Concurrent");
        RubyClass atomicCls = concurrentMod.defineClassUnder("JavaSemaphore", runtime.getObject(), JRUBYREFERENCE_ALLOCATOR);

        atomicCls.defineAnnotatedMethods(JavaSemaphore.class);
    }

    private static final ObjectAllocator JRUBYREFERENCE_ALLOCATOR = new ObjectAllocator() {
        public IRubyObject allocate(Ruby runtime, RubyClass klazz) {
            return new JavaSemaphore(runtime, klazz);
        }
    };

    @JRubyClass(name = "JavaSemaphore", parent = "Object")
    public static class JavaSemaphore extends RubyObject {

        private JRubySemaphore semaphore;

        public JavaSemaphore(Ruby runtime, RubyClass metaClass) {
            super(runtime, metaClass);
        }

        @JRubyMethod
        public IRubyObject initialize(ThreadContext context, IRubyObject value) {
            this.semaphore = new JRubySemaphore(rubyFixnumInt(value, "count"));
            return context.nil;
        }

        @JRubyMethod
        public IRubyObject acquire(ThreadContext context, IRubyObject value) throws InterruptedException {
            this.semaphore.acquire(rubyFixnumToPositiveInt(value, "permits"));
            return context.nil;
        }

        @JRubyMethod(name = "available_permits")
        public IRubyObject availablePermits(ThreadContext context) {
            return getRuntime().newFixnum(this.semaphore.availablePermits());
        }

        @JRubyMethod(name = "drain_permits")
        public IRubyObject drainPermits(ThreadContext context) {
            return getRuntime().newFixnum(this.semaphore.drainPermits());
        }

        @JRubyMethod
        public IRubyObject acquire(ThreadContext context) throws InterruptedException {
            this.semaphore.acquire(1);
            return context.nil;
        }

        @JRubyMethod(name = "try_acquire")
        public IRubyObject tryAcquire(ThreadContext context) throws InterruptedException {
           return getRuntime().newBoolean(semaphore.tryAcquire(1));
        }

        @JRubyMethod(name = "try_acquire")
        public IRubyObject tryAcquire(ThreadContext context, IRubyObject permits) throws InterruptedException {
           return getRuntime().newBoolean(semaphore.tryAcquire(rubyFixnumToPositiveInt(permits, "permits")));
        }

        @JRubyMethod(name = "try_acquire")
        public IRubyObject tryAcquire(ThreadContext context, IRubyObject permits, IRubyObject timeout) throws InterruptedException {
             return getRuntime().newBoolean(
                        semaphore.tryAcquire(
                                rubyFixnumToPositiveInt(permits, "permits"),
                                rubyNumericToLong(timeout, "timeout"),
                                java.util.concurrent.TimeUnit.SECONDS)
                );
        }

        @JRubyMethod
        public IRubyObject release(ThreadContext context) {
            this.semaphore.release(1);
            return getRuntime().newBoolean(true);
        }

        @JRubyMethod
        public IRubyObject release(ThreadContext context, IRubyObject value) {
            this.semaphore.release(rubyFixnumToPositiveInt(value, "permits"));
            return getRuntime().newBoolean(true);
        }

        @JRubyMethod(name = "reduce_permits")
        public IRubyObject reducePermits(ThreadContext context, IRubyObject reduction) throws InterruptedException {
            this.semaphore.publicReducePermits(rubyFixnumToNonNegativeInt(reduction, "reduction"));
            return context.nil;
        }

        private int rubyFixnumInt(IRubyObject value, String paramName) {
            if (value instanceof RubyFixnum) {
                RubyFixnum fixNum = (RubyFixnum) value;
                return (int) fixNum.getLongValue();
            } else {
                throw getRuntime().newArgumentError(paramName + " must be integer");
            }
        }

        private int rubyFixnumToNonNegativeInt(IRubyObject value, String paramName) {
            if (value instanceof RubyFixnum && ((RubyFixnum) value).getLongValue() >= 0) {
                RubyFixnum fixNum = (RubyFixnum) value;
                return (int) fixNum.getLongValue();
            } else {
                throw getRuntime().newArgumentError(paramName + " must be a non-negative integer");
            }
        }

        private int rubyFixnumToPositiveInt(IRubyObject value, String paramName) {
            if (value instanceof RubyFixnum && ((RubyFixnum) value).getLongValue() > 0) {
                RubyFixnum fixNum = (RubyFixnum) value;
                return (int) fixNum.getLongValue();
            } else {
                throw getRuntime().newArgumentError(paramName + " must be an integer greater than zero");
            }
        }

        private long rubyNumericToLong(IRubyObject value, String paramName) {
            if (value instanceof RubyNumeric && ((RubyNumeric) value).getDoubleValue() > 0) {
                RubyNumeric fixNum = (RubyNumeric) value;
                return fixNum.getLongValue();
            } else {
                throw getRuntime().newArgumentError(paramName + " must be a float greater than zero");
            }
        }

        class JRubySemaphore extends Semaphore {

            public JRubySemaphore(int permits) {
                super(permits);
            }

            public JRubySemaphore(int permits, boolean value) {
                super(permits, value);
            }

            public void publicReducePermits(int i) {
                reducePermits(i);
            }

        }
    }
}
