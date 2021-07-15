package com.grafana.loki.batch;

import static org.junit.Assert.assertEquals;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import com.grafana.loki.LokiAppender;
import com.grafana.loki.LokiConfig;
import com.grafana.loki.entry.Entry;
import com.grafana.loki.logproto.Logproto.Stream;
import com.grafana.loki.util.Util;

import ch.qos.logback.classic.Logger;
import ch.qos.logback.classic.LoggerContext;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.core.Context;
import ch.qos.logback.core.ContextBase;

public class BatchTest {

	private static Context context = new ContextBase();

	private LoggerContext loggerContext;

	private Logger logger;

	private LokiAppender<Object> appender = new LokiAppender<Object>();

	private LokiConfig config = new LokiConfig();

	@Before
	public void setup() {
		appender.setLokiConfig(config);
		loggerContext = new LoggerContext();
		loggerContext.setName("testContext");
		logger = loggerContext.getLogger(Logger.ROOT_LOGGER_NAME);
	}

	@After
	public void tearDown() {
		logger = null;
		loggerContext = null;
	}

	@Test
	public void testSingleEntryAddedtoStream() {
		Batch batch = new Batch(context);
		Entry entry = createSingleEntry();
		batch.add(entry);

		Map<String, Stream> streams = batch.getStreams();
		for (Map.Entry<String, Stream> stream : streams.entrySet()) {
			Stream strm = stream.getValue();

			assertEquals(strm.getLabels(), entry.getLabels().toString());
			assertEquals(strm.getEntries(0), entry.getLogprotoEntry());
		}
	}

	@Test
	public void testMultipleEntriesAddedtoSingleStreamsMap() {
		Batch batch = new Batch(context);
		LinkedHashMap<String, List<com.grafana.loki.logproto.Logproto.Entry>> entriesMap = new LinkedHashMap<String, List<com.grafana.loki.logproto.Logproto.Entry>>();

		createMultipleEntries(batch, entriesMap);

		Map<String, Stream> streams = batch.getStreams();
		assertEquals(streams.size(), 1);

		// sort the map so that key,values are always ordered
		LinkedHashMap<String, Stream> sortedStreams = new LinkedHashMap<>();
		streams.entrySet().stream().sorted(Map.Entry.comparingByKey())
				.forEachOrdered(x -> sortedStreams.put(x.getKey(), x.getValue()));

		List<String> keyList = new ArrayList<String>(sortedStreams.keySet());
		List<String> keyListExpected = new ArrayList<String>(entriesMap.keySet());

		for (int i = 0; i < keyList.size(); i++) {
			String key = keyList.get(i);
			String keyExpected = keyListExpected.get(i);

			List<com.grafana.loki.logproto.Logproto.Entry> expectedEntries = entriesMap.get(keyExpected);
			List<com.grafana.loki.logproto.Logproto.Entry> receivedEntries = sortedStreams.get(key).getEntriesList();

			assertEquals(keyExpected, key);
			assertEquals(expectedEntries, receivedEntries);
		}
	}

	@Test
	public void testMultipleEntriesAddedtoDifferentStreamsMap() {
		Batch batch = new Batch(context);
		LinkedHashMap<String, List<com.grafana.loki.logproto.Logproto.Entry>> entriesMap = new LinkedHashMap<String, List<com.grafana.loki.logproto.Logproto.Entry>>();

		createMultipleEntries(batch, entriesMap);
		createMultipleEntriesWithInfo(batch, entriesMap);

		Map<String, Stream> streams = batch.getStreams();
		assertEquals(streams.size(), 2);

		// sort the map so that key,values are always ordered
		LinkedHashMap<String, Stream> sortedStreams = new LinkedHashMap<>();
		streams.entrySet().stream().sorted(Map.Entry.comparingByKey())
				.forEachOrdered(x -> sortedStreams.put(x.getKey(), x.getValue()));

		List<String> keyList = new ArrayList<String>(sortedStreams.keySet());
		List<String> keyListExpected = new ArrayList<String>(entriesMap.keySet());

		for (int i = 0; i < keyList.size(); i++) {
			String key = keyList.get(i);
			String keyExpected = keyListExpected.get(i);

			List<com.grafana.loki.logproto.Logproto.Entry> expectedEntries = entriesMap.get(keyExpected);
			List<com.grafana.loki.logproto.Logproto.Entry> receivedEntries = sortedStreams.get(key).getEntriesList();

			assertEquals(keyExpected, key);
			assertEquals(expectedEntries, receivedEntries);
		}
	}

	private Entry createSingleEntry() {
		ILoggingEvent event = Util.createLoggingEvent(this, logger);
		Entry entry = appender.buildLokiEntry(event);
		return entry;
	}

	private void createMultipleEntries(Batch batch,
			LinkedHashMap<String, List<com.grafana.loki.logproto.Logproto.Entry>> entriesMap) {
		List<com.grafana.loki.logproto.Logproto.Entry> entries = new ArrayList<com.grafana.loki.logproto.Logproto.Entry>();
		Entry entry = null;
		for (int i = 0; i < 3; i++) {
			ILoggingEvent event = Util.createLoggingEventWithMessage(this, logger, "test message " + String.valueOf(i));
			entry = appender.buildLokiEntry(event);
			entries.add(entry.getLogprotoEntry());
			batch.add(entry);
		}

		entriesMap.put(entry.getLabels().toString(), entries);
	}

	private void createMultipleEntriesWithInfo(Batch batch,
			LinkedHashMap<String, List<com.grafana.loki.logproto.Logproto.Entry>> entriesMap) {
		List<com.grafana.loki.logproto.Logproto.Entry> entries = new ArrayList<com.grafana.loki.logproto.Logproto.Entry>();
		Entry entry = null;
		for (int i = 0; i < 3; i++) {
			ILoggingEvent event = Util.createLoggingEventWithInfo(this, logger);
			entry = appender.buildLokiEntry(event);
			entries.add(entry.getLogprotoEntry());
			batch.add(entry);
		}

		entriesMap.put(entry.getLabels().toString(), entries);
	}
}
