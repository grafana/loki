package com.grafana.loki.util;

import ch.qos.logback.classic.Level;
import ch.qos.logback.classic.Logger;
import ch.qos.logback.classic.spi.LoggingEvent;

public class Util {
	public static LoggingEvent createLoggingEvent(Object object, Logger logger) {
		return new LoggingEvent(object.getClass().getName(), logger, Level.DEBUG, "test message", null, null);
	}
	
	public static LoggingEvent createLoggingEventWithThrowable(Object object, Logger logger, Throwable t) {
        return new LoggingEvent(object.getClass().getName(), logger, Level.DEBUG, "test message", t, null);
    }
	
	public static LoggingEvent createLoggingEventWithInfo(Object object, Logger logger) {
		return new LoggingEvent(object.getClass().getName(), logger, Level.INFO, "test message", null, null);
	}
	
	public static LoggingEvent createLoggingEventWithMessage(Object object, Logger logger, String message) {
		return new LoggingEvent(object.getClass().getName(), logger, Level.DEBUG, message, null, null);
	}
	
	public static String setLabelString(String value) {
		return "\"" + value + "\"";
	}
}
