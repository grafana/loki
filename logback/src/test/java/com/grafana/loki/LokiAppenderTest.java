package com.grafana.loki;

import static org.junit.Assert.assertEquals;

import java.util.Date;
import java.util.HashMap;
import java.util.Map;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import com.google.protobuf.Timestamp;
import com.grafana.loki.entry.Entry;
import com.grafana.loki.logproto.Logproto.Entry.Builder;
import com.grafana.loki.util.Util;

import ch.qos.logback.classic.Logger;
import ch.qos.logback.classic.LoggerContext;
import ch.qos.logback.classic.pattern.CallerDataConverter;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.classic.spi.ThrowableProxyUtil;
import ch.qos.logback.core.Context;
import ch.qos.logback.core.ContextBase;

public class LokiAppenderTest {
	Context context = new ContextBase();
	private LokiAppender<Object> appender = new LokiAppender<Object>();
	private LokiConfig config = new LokiConfig();

	LoggerContext loggerContext;
	Logger logger;

	@Before
	public void setUp() throws Exception {
		appender.setContext(context);
		appender.setLokiConfig(config);
		config.setUrl("http://localhost:3100/loki/api/v1/push");
		loggerContext = new LoggerContext();
		loggerContext.setName("testContext");
		logger = loggerContext.getLogger(Logger.ROOT_LOGGER_NAME);
	}

	@After
	public void tearDown() {
		loggerContext = null;
		logger = null;
	}

	@Test
	public void testLabelsExtracted() throws Exception {
		ILoggingEvent event = Util.createLoggingEvent(this, logger);
		Map<String, String> expectedLabels = new HashMap<String, String>();

		expectedLabels.put("logback_log_logger", Util.setLabelString("ROOT"));
		expectedLabels.put("logback_log_thread", Util.setLabelString("main"));
		expectedLabels.put("logback_log_level", Util.setLabelString("DEBUG"));

		Map<String, String> receivedLabels = appender.extractLabels(event);

		assertEquals(expectedLabels, receivedLabels);
	}

	@Test
	public void testLokiEntry() {
		ILoggingEvent event = Util.createLoggingEvent(this, logger);

		Entry expectedEntry = getExpectedEntry(event);

		Entry receivedEntry = appender.buildLokiEntry(event);

		assertEquals(expectedEntry.getLabels(), receivedEntry.getLabels());
		assertEquals(expectedEntry.getLogprotoEntry(), receivedEntry.getLogprotoEntry());
	}

	@Test
	public void testLokiEntryWithThrowable() {
		Throwable throwable = new Throwable("testing throwable");
		ILoggingEvent event = Util.createLoggingEventWithThrowable(this, logger, throwable);
		
		Entry expectedEntry = getExpectedEntry(event);

		Entry receivedEntry = appender.buildLokiEntry(event);

		assertEquals(expectedEntry.getLabels(), receivedEntry.getLabels());
		assertEquals(expectedEntry.getLogprotoEntry(), receivedEntry.getLogprotoEntry());
	}

	@Test
	public void testLokiEntryWithMarker() {
		Throwable throwable = new Throwable("testing throwable");
		ILoggingEvent event = Util.createLoggingEventWithThrowable(this, logger, throwable);
		event.getCallerData();
		Entry expectedEntry = getExpectedEntry(event);
		Entry receivedEntry = appender.buildLokiEntry(event);

		assertEquals(expectedEntry.getLabels(), receivedEntry.getLabels());
		assertEquals(expectedEntry.getLogprotoEntry(), receivedEntry.getLogprotoEntry());
	}

	@Test
	public void testLokiEntryWithLabels() {
		Map<String, String> externalLabels = new HashMap<String, String>();
		externalLabels.put("test", "value");
		config.setLabels(externalLabels);

		Throwable throwable = new Throwable("testing throwable");
		ILoggingEvent event = Util.createLoggingEventWithThrowable(this, logger, throwable);
		
		Entry expectedEntry = getExpectedEntry(event);

		Entry receivedEntry = appender.buildLokiEntry(event);

		assertEquals(expectedEntry.getLabels(), receivedEntry.getLabels());
		assertEquals(expectedEntry.getLogprotoEntry(), receivedEntry.getLogprotoEntry());
	}
	
	private Entry getExpectedEntry(ILoggingEvent event) {	
		Map<String, String> labels = new HashMap<String, String>();

		labels.put("logback_log_logger", Util.setLabelString("ROOT"));
		labels.put("logback_log_thread", Util.setLabelString("main"));
		labels.put("logback_log_level", Util.setLabelString("DEBUG"));
		if (event.hasCallerData()) {
			labels.put("logback_log_caller", Util.setLabelString(new CallerDataConverter().convert(event).toString()));
		}
		
		Builder logprotoEntryBuilder = com.grafana.loki.logproto.Logproto.Entry.newBuilder();

		StringBuilder sb = new StringBuilder();
		sb.append(event.getFormattedMessage());

		if (event.getThrowableProxy() != null) {
			sb.append(ThrowableProxyUtil.asString(event.getThrowableProxy()));
		}

		logprotoEntryBuilder.setLine(sb.toString());

		Date d = new Date(event.getTimeStamp());
		logprotoEntryBuilder.setTimestamp(Timestamp.newBuilder().setSeconds(d.toInstant().getEpochSecond()));
		
		if(config.getLabels() != null) {
			labels.putAll(config.getLabels());
		}
		
		Entry expectedEntry = new Entry(labels, logprotoEntryBuilder.build());
		return expectedEntry;

	}

}
