package com.grafana.loki;

import java.io.FileInputStream;
import java.io.InputStream;
import java.security.KeyStore;
import java.security.SecureRandom;
import java.util.Date;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;

import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManagerFactory;

import org.apache.http.client.utils.URIBuilder;
import org.slf4j.Marker;

import com.google.protobuf.Timestamp;
import com.grafana.loki.entry.Entry;
import com.grafana.loki.logproto.Logproto.Entry.Builder;

import ch.qos.logback.classic.pattern.CallerDataConverter;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.classic.spi.ThrowableProxyUtil;
import ch.qos.logback.core.AppenderBase;
import ch.qos.logback.core.Context;

public class LokiAppender<E> extends AppenderBase<E> {

	protected Map<String, String> additionalFields;

	protected BlockingQueue<Entry> entryQueue = new LinkedBlockingQueue<Entry>();

	protected BlockingQueue<Long> ticker = new LinkedBlockingQueue<Long>();

	protected Thread tickerThread;

	protected Thread batchThread;

	protected Ticker timeTicker;

	protected BatchHandler batchHandler;

	public LokiConfig config;

	protected Context ctx;

	private static final String LOGGER = "logback_log_logger";

	private static final String THREAD = "logback_log_thread";

	private static final String LEVEL = "logback_log_level";

	private static final String CALLER = "logback_log_caller";

	private static final String MARKER = "logback_log_marker";

	@Override
	public void start() {
		addInfo("Starting Grafana-Loki logback appender");

		if (!isValidURL()) {
			return;
		}

		if (!isSSLContextValid()) {
			return;
		}

		ctx = getContext();

		timeTicker = new Ticker(ticker, config, ctx);
		tickerThread = new Thread(timeTicker);
		tickerThread.start();

		batchHandler = new BatchHandler(entryQueue, ticker, config, ctx);
		batchThread = new Thread(batchHandler);
		batchThread.start();

		super.start();
	}

	@Override
	public void stop() {
		try {
			addInfo("Stopping Grafana-Loki logback appender");
			if (this.started) {
				if (tickerThread != null) {
					timeTicker.terminate();
					tickerThread.join();
					addInfo("Closed ticker");

				}

				if (batchThread != null) {
					batchHandler.terminate();
					batchThread.join();
					addInfo("Closed batch handler. Flush any pending batch");
					if (batchHandler.getBatch() != null) {
						batchHandler.send(config.getTenantID(), batchHandler.getBatch());
					}
				}
				super.stop();
			}

		} catch (InterruptedException e) {
			addError("Error stopping appender. Some logs might not be sent to loki", e);
		} finally {
			addInfo("Stopped Grafana-Loki logback appender");
		}
	}

	@Override
	protected void append(E event) {
		if (event instanceof ILoggingEvent) {
			ILoggingEvent loggingEvent = (ILoggingEvent) event;
			handleEvent(loggingEvent);
		}
	}

	protected void handleEvent(ILoggingEvent event) {
		Entry entry = buildLokiEntry(event);

		try {
			entryQueue.put(entry);
		} catch (InterruptedException e) {
			addError("error adding entry to queue", e);
		}

	}

	public Entry buildLokiEntry(ILoggingEvent event) {
		Map<String, String> labels = extractLabels(event);

		Builder logprotoEntryBuilder = com.grafana.loki.logproto.Logproto.Entry.newBuilder();

		StringBuilder sb = new StringBuilder();
		sb.append(event.getFormattedMessage());

		if (event.getThrowableProxy() != null) {
			sb.append(ThrowableProxyUtil.asString(event.getThrowableProxy()));
		}

		logprotoEntryBuilder.setLine(sb.toString());

		Date d = new Date(event.getTimeStamp());
		logprotoEntryBuilder.setTimestamp(Timestamp.newBuilder().setSeconds(d.toInstant().getEpochSecond()));

		if (config.getLabels() != null) {
			labels.putAll(config.getLabels());
		}

		return new Entry(labels, logprotoEntryBuilder.build());
	}

	public Map<String, String> extractLabels(ILoggingEvent event) {
		Map<String, String> labels = new HashMap<String, String>();
		labels.put(LOGGER, setLabelString(event.getLoggerName().toString()));
		labels.put(THREAD, setLabelString(event.getThreadName().toString()));
		labels.put(LEVEL, setLabelString(event.getLevel().levelStr.toString()));

		if (event.hasCallerData()) {
			labels.put(CALLER, setLabelString(new CallerDataConverter().convert(event).toString()));
		}

		for (Map.Entry<String, String> entry : event.getMDCPropertyMap().entrySet()) {
			labels.put(entry.getKey(), setLabelString(entry.getValue().toString()));
		}

		Marker marker = event.getMarker();
		if (marker != null) {
			labels.put(MARKER, setLabelString(marker.toString()));
		}

		return labels;
	}

	public LokiConfig getLokiConfig() {
		return config;
	}

	public void setLokiConfig(LokiConfig config) {
		this.config = config;
	}

	private String setLabelString(String value) {
		return "\"" + value + "\"";
	}

	private boolean isValidURL() {
		if (config.getUrl() != null) {
			try {
				URIBuilder url = new URIBuilder(config.getUrl());
				if (!url.getScheme().toString().equals("http") && !url.getScheme().toString().equals("https")) {
					addError("Scheme provided in the URL is invalid: " + config.getUrl());
					return false;
				}
				context.putObject("url", url.build());
				return true;
			} catch (Exception e) {
				addError("Error in provided URL: " + config.getUrl() + " ", e);
				return false;
			}
		}
		addError("url field cannot be empty");
		return false;
	}

	private boolean isSSLContextValid() {
		if (config.getTrustStore() != null) {
			try {
				KeyStore keystore = KeyStore.getInstance(KeyStore.getDefaultType());

				InputStream in = new FileInputStream(config.getTrustStore());
				keystore.load(in, config.getTrustStorePassword().toCharArray());

				KeyManagerFactory keyManagerFactory = KeyManagerFactory
						.getInstance(KeyManagerFactory.getDefaultAlgorithm());
				keyManagerFactory.init(keystore, config.getTrustStorePassword().toCharArray());

				TrustManagerFactory trustManagerFactory = TrustManagerFactory
						.getInstance(TrustManagerFactory.getDefaultAlgorithm());
				trustManagerFactory.init(keystore);

				SSLContext sslContext = SSLContext.getInstance("TLS");
				sslContext.init(keyManagerFactory.getKeyManagers(), trustManagerFactory.getTrustManagers(),
						new SecureRandom());
				context.putObject("sslContext", sslContext);
				return true;
			} catch (Exception e) {
				addError("Error configuring ssl context: ", e);
				return false;
			}
		}
		return true;
	}

}