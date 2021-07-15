package com.grafana.loki.http;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockserver.integration.ClientAndServer.startClientAndServer;
import static org.mockserver.matchers.Times.exactly;
import static org.mockserver.model.HttpRequest.request;
import static org.mockserver.model.HttpResponse.response;

import java.io.IOException;
import java.net.URISyntaxException;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.mockserver.client.MockServerClient;
import org.mockserver.configuration.ConfigurationProperties;
import org.mockserver.integration.ClientAndServer;
import org.mockserver.model.HttpRequest;
import org.slf4j.LoggerFactory;
import org.xerial.snappy.Snappy;

import com.grafana.loki.LokiAppender;
import com.grafana.loki.LokiConfig;
import com.grafana.loki.entry.Entry;
import com.grafana.loki.logproto.Logproto.PushRequest;
import com.grafana.loki.util.Util;

import ch.qos.logback.classic.Level;
import ch.qos.logback.classic.Logger;
import ch.qos.logback.classic.LoggerContext;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.core.Context;
import ch.qos.logback.core.ContextBase;

public class HttpTest {

	private static Context context = new ContextBase();

	private LoggerContext loggerContext;

	private Logger logger;

	private ClientAndServer mockServer;

	private LokiAppender<Object> appender = new LokiAppender<Object>();

	private LokiConfig config = new LokiConfig();

	@Before
	public void setup() throws InterruptedException {
		ConfigurationProperties.logLevel("OFF");
		config.setUrl("http://127.0.0.1:1080/loki/api/v1/push");
		appender.setContext(context);
		appender.setLokiConfig(config);
		loggerContext = (LoggerContext) LoggerFactory.getILoggerFactory();
		loggerContext.setName("testContext");
		logger = loggerContext.getLogger(Logger.ROOT_LOGGER_NAME);
		logger.setLevel(Level.WARN);
		mockServer = startClientAndServer(1080);
	}

	@After
	public void tearDown() {
		mockServer.stop();
		logger = null;
		loggerContext = null;
		appender = null;
		config = null;
	}

	@Test
	public void testRequestsAreEqual() throws IOException, URISyntaxException, InterruptedException {
		createExpectation();

		appender.start();
		assertTrue(appender.isStarted());

		ILoggingEvent event = Util.createLoggingEvent(this, logger);
		appender.doAppend(event);

		PushRequest pushRequest = createPushRequest();

		Thread.sleep(2000); // wait to reach default max batch_wait time

		HttpRequest[] recordedRequests = getRecordedRequests();
		for (HttpRequest request : recordedRequests) {
			byte[] uncompressedRequest = Snappy.uncompress(request.getBodyAsRawBytes());
			assertEquals(new String(pushRequest.toByteArray()), new String(uncompressedRequest));
		}
		appender.stop();
	}

	@Test
	public void testResponseNull() throws IOException, URISyntaxException, InterruptedException {
		createExpectation();

		config.setBatchWaitSeconds(3);
		appender.start();
		assertTrue(appender.isStarted());

		ILoggingEvent event = Util.createLoggingEvent(this, logger);
		appender.doAppend(event);

		Thread.sleep(1000); // wait less than max batch_wait time

		HttpRequest[] recordedRequests = getRecordedRequests();
		assertEquals(recordedRequests.length, 0);
		appender.stop();
	}

	private com.grafana.loki.logproto.Logproto.PushRequest createPushRequest() throws IOException {
		ILoggingEvent event = Util.createLoggingEvent(this, logger);
		Entry entry = appender.buildLokiEntry(event);

		com.grafana.loki.logproto.Logproto.Stream.Builder streamBuilder = com.grafana.loki.logproto.Logproto.Stream
				.newBuilder();
		streamBuilder.setLabels(entry.getLabels().toString());
		streamBuilder.addEntries(entry.getLogprotoEntry());

		com.grafana.loki.logproto.Logproto.PushRequest.Builder pushRequest = PushRequest.newBuilder();
		pushRequest.addStreams(streamBuilder.build());
		return pushRequest.build();

	}

	@SuppressWarnings("resource")
	private void createExpectation() {
		new MockServerClient("127.0.0.1", 1080).when(request().withMethod("POST").withPath("/loki/api/v1/push")
				.withHeader("Content-type", "application/x-protobuf"), exactly(1))
				.respond(response().withStatusCode(204));
	}

	@SuppressWarnings("resource")
	private HttpRequest[] getRecordedRequests() {
		return new MockServerClient("localhost", 1080).retrieveRecordedRequests(request().withMethod("POST")
				.withPath("/loki/api/v1/push").withHeader("Content-type", "application/x-protobuf"));
	}

}
