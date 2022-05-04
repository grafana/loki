package syslog

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-kit/log"
	"github.com/influxdata/go-syslog/v3"
	promconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/syslog/syslogparser"
)

var (
	caCert = []byte(`
-----BEGIN CERTIFICATE-----
MIIFDTCCAvWgAwIBAgIRAKybTDGpnijNRJOBzgQCurIwDQYJKoZIhvcNAQELBQAw
IDEeMBwGA1UEAxMVUHJvbXRhaWwgVGVzdCBSb290IENBMB4XDTIxMDYyNzE2NTMz
MloXDTI2MDYyNjE2NTMzMlowIDEeMBwGA1UEAxMVUHJvbXRhaWwgVGVzdCBSb290
IENBMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA2tNN84TdJJ1olhPL
zpXJLKUxHEmZ41+2SGy1hzwfsM0XIXjxWIMio/n/ZVym5Yk6ZLir9+L6bUpMZTYl
hwYHwSPQEOK1Y1Kkix++uEZP70Yg7keyW8nRWRnyZT5C0jH6jZI/k94cZUhLlDjY
iMm8IXcLp6Dp5fBbM9/ZLb1djsZ+//9z5QS66e8UrOAmdSgjjJmMr7nLJEx0nJpC
xQHsoKrvmGQ4dy3ro2fINioaHPq5ujk2wAEu0XnkQAjZkpmXcTmAnNaWJswQks/Z
Dqp5Oh2EiwO6aN88fauM7Xb/emwY+u1hyk5HSNuxbpOOUrx939HUgqIka4bsWbK0
GwFlvuzrWP2f4hIIfcMoEVC+oGW3NfgD2drq+dBrmfRGXpSxBNrUwCvU8cweDe/3
xAeI1fjB8JNMcyWWblHG5YbmE4MHMNdS7eUvGrlunmCcCLSvvwbEUpMCwUof0YBI
RLhaOFSLEFFS0qWoJwwgg45OqilgPuVAWJ+WjjzWwlu2BD8C1uUeAEJx1L0a0rp0
3fdWmXf97AWTjC3oIZK9wkxj1dSxsEDCcrq9AHdvvpn8g5Cjr5MWteImMrHYcwe1
Gleb46eM0PwpdkLLmok0sai/SnjDwZu0ABMr0oKz2/m+GxIL1Ebk+XMMnQacMn/8
l0uGpY69Dx1mpXTJQiXSNq84JjECAwEAAaNCMEAwDgYDVR0PAQH/BAQDAgKkMA8G
A1UdEwEB/wQFMAMBAf8wHQYDVR0OBBYEFKoQ/kE3BMM/QA3BcF0zqNDSfLf1MA0G
CSqGSIb3DQEBCwUAA4ICAQBIpihcbcsRSilMENFkBpbKqlHK3FmNPhxbMSVp5uFy
/Ke5hS5rg+IDW/0KHCM2QXjEs0ZreOzbc+4/jHDa+bzZW2cBqPf7yISoE6c0uZ4d
mMxTRtSJbD6PG2hvlkD8M+I+bq0BP677PRblfw/ASGaQzy4SBSnVRXPlMohe91zT
ArI7N0nEfHzTi+wEhHXJi++qLO4WRXZnZKfAvJ54qbyRMZSZXV3VvVmZYfKT1MuF
WhNJDR0lIjr2Er5q807ev1556mjiq5vwu1dFmIGX8IIb9gjjyrZYvr1Sh3h8RVZh
mW0MlmWf5/tQ5JvKQw44F7ynBEYt1C2i+hQsfFO9cfGXtU3PKnoFjxZ7dDpy8hK2
91qhkUf9B29tX2Dm/fd0BknoS7t8bOVDls0sGAsmg4EEEIdFUdKn/0vQx67Cog33
GOIfFHujBC7LI+HUu/NpAp1V/hnqm8R6DQSntITJWTICklEZfbTowq2bdcjDUILL
YU4QdgyJHGetIOAabUzV74Q7WQ/1YiihSDGLbykQ03bYP5B+rkkF3/eSVwZMzMll
olUQe0fCwHF8n4imr77F/1U/s3ODKPuRB9hr7zqsboDK7OzjiJoFlPIjmA6tCVlJ
6JAHwLVqTztdUTwkc9bMY3ieIIfyLIaJ7wq4igbL33SCYl3Yxwj3ZA9sj3zrUoIu
ig==
-----END CERTIFICATE-----
`)

	serverCert = []byte(`
-----BEGIN CERTIFICATE-----
MIIFWDCCA0CgAwIBAgIRANu9qwosWKYNIYxSVbBJla8wDQYJKoZIhvcNAQELBQAw
IDEeMBwGA1UEAxMVUHJvbXRhaWwgVGVzdCBSb290IENBMB4XDTIxMDYyNzE2NTY1
NVoXDTIyMDYyNzE2NTY1NVowKzEpMCcGA1UEAxMgUHJvbXRhaWwgVGVzdCBTZXJ2
ZXIgQ2VydGlmaWNhdGUwggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQDV
fb1d5g4v7yVlFI/wvuDV8RirmY7bYtLa/z7lk8QYlVB2fA2K8PGk+zd/O2wTjYGf
Zn8Q3ttW9HGFYJSsi1Jel57ANW6vToPy3u2fmwjOUWiRvYVcl8rz32sdmUYx8wyV
nb89bFV3lW0vIHQljmd2z9sL/xHY7ZkViZ4+cd5OnHm3Y46ZKmtmIPHyPiHKGGT9
d53k2ooIsZ3PjVGU4hwfKwfYQ982hBXWGEbv6LjNGuKP+0JqCbgFOfurHd25xXTL
yzFaICVjXjIm5AriGmpSIWpg3YIEl/Jbe70MhWpSw18SjawEHCES+e2jZgqQ/QcZ
uk+fy8zv4PRnunoiSEWyQm+G0ajkaW1gQRIAtlHNth1M8PHgVinZE+ghu5a7UJuN
dT4/UH+Bwzin7NfIjmZAIbIq9xW+OreMGF3K83DPmg96DUQbyMyx0q49DSBmAtHl
Mb/uvy2AKfIQRZgxi0In167zHTcev7L8/kFbJVn55r7IURK7gjp/iyplC2zI24Tr
+DeqTT3+r3BTn4x+4HIa+sK6BrLq0kLDBgksOYTv0ilJp4JDv4YT4/FUSGLt6uA4
CjHL0SLX3nnYM66bx2wwFB0OkJPtfo1h0X/YafNDoDKrZ9BmOn18BTSxV3utyiEG
UAnNSqzGTTLu29uHQ3dkdmrXNdwYZVzpz5kE2DmGswIDAQABo4GBMH8wDgYDVR0P
AQH/BAQDAgWgMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAMBgNVHRMB
Af8EAjAAMB8GA1UdIwQYMBaAFKoQ/kE3BMM/QA3BcF0zqNDSfLf1MB8GA1UdEQQY
MBaCFHByb210YWlsLmV4YW1wbGUuY29tMA0GCSqGSIb3DQEBCwUAA4ICAQAbnorG
nvMi46fC9gVUDMBLsQluVb26rU+OV1usSjkRzW16c1FTJJriGIRSmkK1irH9Ff9e
GFowpvEMGDKBHNcMlbjxfHyThfFYI+v5J77NaytN4v4vJhhF1tFfqkI19YUCcZYQ
lcMnV5IAKh3r+fYRmHm3ey1WV96eHApzXui4+kUeEWiUN+x0J5ApTRFasqFArLVw
3c0ARaW0jkixl01qLt2VzpDP/GLwK/2wl+n0LSFHyQUeZpY69DR5H2tFgCvc6n4B
qJwy5AesMdxefnIjuRELcG1sjaybB7j5YghadzR2xMLtz2iCCXtBdcKH6x+GTj1O
PQo5Jm3//5b9rwIZl0EQnnXwWrHrIw5nbBX7gbQ2FNqdrHyj/uy3WcxaLhNrEadC
muB9cCov3U0McqDbUdZG0ZhZhrAqbiCHwJY2BOPVLsl/lP0pHI//DOLXMyHSaFDW
dFoUL/fAn7ZNhLATf7sRYFlPwmiO2VTrVzJ7+Ak8RW9c3L598RHvM3KGXC0ahHr2
hnOrHYYiJK5tZL1kGNrokmww5n1L2mkALqCH9eSztHJFt9VGpGkrCd+3hI1GpuE2
4z2QFaE6vVC7GxxmnQChIVRZS8KGuDQ4uPkTcnnVlOPxawquVY05e2lxdJBItgVQ
Wu4omM2NavzTD8mlK74W7B8qKq+NMHGxAUEmHg==
-----END CERTIFICATE-----
`)

	serverKey = []byte(`
-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----
`)
	clientCert = []byte(`
-----BEGIN CERTIFICATE-----
MIIFNTCCAx2gAwIBAgIQLcRrnKNMcTvcPl93lSKsnjANBgkqhkiG9w0BAQsFADAg
MR4wHAYDVQQDExVQcm9tdGFpbCBUZXN0IFJvb3QgQ0EwHhcNMjEwNjI3MTY1NjU5
WhcNMjIwNjI3MTY1NjU5WjArMSkwJwYDVQQDEyBQcm9tdGFpbCBUZXN0IENsaWVu
dCBDZXJ0aWZpY2F0ZTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBANAo
OvLXU5dZGVOt8dBcVPWAWaikPK/pDLNrXAcQ+vINLpcyKd1bcRRSfYm+q8f4kKt8
x+gWWjAtRcVvXBttKSS6834X7GhI0oH3qiKuQlha7NGBHQJeZSAPAwogS7/l8z4d
lJOnUoLTPe/EUeCVo6WgooRunGf6UtymApJFl6i0aQo7Y6qevLSg8uWaRX8/wYPW
HOzDZpbwdTVYpMnj1mOTcA+YF/M1QFRoBrXGL9Oan/1ecwuF37Ho8uCH0Fx98vsk
jMThn/VnU0dRH9Jx05inuxCm/z4e3tHg0esjyY0kmum6gCdQufMdqDWHJABY2H+1
dqvtoNIYK5gIkaRWNOWV6F5H9+kHgcKPQMfYUGPxEwDnWQ+DInYZ128Gm5bYKa81
pFs4XY5d6TOmt00Bs28fRMOfdRi16sHTUswl/8wlx1MIHofKgEEUta8r9FqxUsIT
duwcZ6LwPa6xDuFl30kW6I4Oa3UHUnOD6WgEL+BycNHTJBpwZLnXrq0rG5NRQS+A
36krW2EkHavTrFE1eFmkAul4tJSw2jCiSHmenj54ycGMcU5Bn7G/wFNH8nW7ywlC
LTdjK4goMtAJ52aPT3G/WW2bL8jlBpnL4+lWwnpfugDtckdFpXPFF9WV30nH95Yr
813Vi1dbEOMF56E9FaUTT8eHftqzkK4ZlPmRrN+BAgMBAAGjYDBeMA4GA1UdDwEB
/wQEAwIFoDAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDAYDVR0TAQH/
BAIwADAfBgNVHSMEGDAWgBSqEP5BNwTDP0ANwXBdM6jQ0ny39TANBgkqhkiG9w0B
AQsFAAOCAgEAs6DQcP027wIPyIOq1UMVA2koxt6sGECgO897Z07dRIEd3/Oihc7L
BQCGnqtjxKTC1yfvakCQHRADxos2uTt0l5If3TxLSu4Lx16Nk+ApwwMbfTd8jmXT
tNyZTrgEcE8bJVnv/N38Ur7puJypl5S8Z9QmzNSQYGdF4f9Ngab7MOoIv/gqhVty
X7Lk+yWom69vVOsuisSfbD+fo02Cf4jcLVCKcTwWqv10S2v1r9hjM6B9i2NkERih
ryYViEPFdsQ36lA4Z4xs3JCYxUqAOHzASIAn1cBTZUT9tu2A3UZTK1x57IR8q583
M0wc9/ZgGPxjLMrRbFJOFM4CX08vC482ZgdWgyQmIhMu8ama1DMje6cZN4QFo58k
btGMdJ2jFBYCssDix9/RHG5fAdU2PrtqShwyn4TinFDPpTXr1VlON3l9mFXP0sDb
KiZXzKtDCktaKRcBFaLgKfmrfASVZ5z/9z67A+XY+78kfbGwpDUIGFNsCtGRLq66
kUC2qrrXgM51toJpcW5Rdm74LOAMPCQg0raCM67Ou0rPE9ucSskexB6X6oc7mkop
JO8VEMoObWB+0ftjNsL3fUCUTyimdvieb6LIcyrzZBpcqzlfFgQ/cbbR2+P1D7s7
rWAq4Upb+WRl6uDfmMiCgHHwketUuBEZUmkf+LT4OZcTYzv6AzL1ApA=
-----END CERTIFICATE-----
`)
	clientKey = []byte(`
-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----
`)
)

type formatFunc func(string) string

var (
	fmtOctetCounting = func(s string) string { return fmt.Sprintf("%d %s", len(s), s) }
	fmtNewline       = func(s string) string { return s + "\n" }
)

func Benchmark_SyslogTarget(b *testing.B) {
	for _, tt := range []struct {
		name       string
		protocol   string
		formatFunc formatFunc
	}{
		{"tcp", protocolTCP, fmtOctetCounting},
		{"udp", protocolUDP, fmtOctetCounting},
	} {
		tt := tt
		b.Run(tt.name, func(b *testing.B) {
			client := fake.New(func() {})

			metrics := NewMetrics(nil)
			tgt, _ := NewSyslogTarget(metrics, log.NewNopLogger(), client, []*relabel.Config{}, &scrapeconfig.SyslogTargetConfig{
				ListenAddress:       "127.0.0.1:0",
				ListenProtocol:      tt.protocol,
				LabelStructuredData: true,
				Labels: model.LabelSet{
					"test": "syslog_target",
				},
			})
			b.Cleanup(func() {
				require.NoError(b, tgt.Stop())
			})
			require.Eventually(b, tgt.Ready, time.Second, 10*time.Millisecond)

			addr := tgt.ListenAddress().String()

			messages := []string{
				`<165>1 2022-04-08T22:14:10.001Z host1 app - id1 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2022-04-08T22:14:11.002Z host2 app - id2 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2022-04-08T22:14:12.003Z host1 app - id3 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2022-04-08T22:14:13.004Z host2 app - id4 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2022-04-08T22:14:14.005Z host1 app - id5 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2022-04-08T22:14:15.002Z host2 app - id6 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2022-04-08T22:14:16.003Z host1 app - id7 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2022-04-08T22:14:17.004Z host2 app - id8 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2022-04-08T22:14:18.005Z host1 app - id9 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2022-04-08T22:14:19.001Z host2 app - id10 [custom@32473 exkey="1"] An application event log entry...`,
			}

			b.ReportAllocs()
			b.ResetTimer()

			c, _ := net.Dial(tt.protocol, addr)
			for n := 0; n < b.N; n++ {
				_ = writeMessagesToStream(c, messages, tt.formatFunc)
			}
			c.Close()

			require.Eventuallyf(b, func() bool {
				return len(client.Received()) == len(messages)*b.N
			}, 15*time.Second, time.Second, "expected: %d got:%d", len(messages)*b.N, len(client.Received()))

		})
	}
}

func TestSyslogTarget(t *testing.T) {
	for _, tt := range []struct {
		name     string
		protocol string
		fmtFunc  formatFunc
	}{
		{"tpc newline separated", protocolTCP, fmtNewline},
		{"tpc octetcounting", protocolTCP, fmtOctetCounting},
		{"udp newline separated", protocolUDP, fmtNewline},
		{"udp octetcounting", protocolUDP, fmtOctetCounting},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := log.NewSyncWriter(os.Stderr)
			logger := log.NewLogfmtLogger(w)
			client := fake.New(func() {})

			metrics := NewMetrics(nil)
			tgt, err := NewSyslogTarget(metrics, logger, client, relabelConfig(t), &scrapeconfig.SyslogTargetConfig{
				MaxMessageLength:    1 << 12, // explicitly not use default value
				ListenAddress:       "127.0.0.1:0",
				ListenProtocol:      tt.protocol,
				LabelStructuredData: true,
				Labels: model.LabelSet{
					"test": "syslog_target",
				},
			})
			require.NoError(t, err)

			require.Eventually(t, tgt.Ready, time.Second, 10*time.Millisecond)

			addr := tgt.ListenAddress().String()
			c, err := net.Dial(tt.protocol, addr)
			require.NoError(t, err)

			messages := []string{
				`<165>1 2018-10-11T22:14:15.003Z host5 e - id1 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2018-10-11T22:14:15.005Z host5 e - id2 [custom@32473 exkey="2"] An application event log entry...`,
				`<165>1 2018-10-11T22:14:15.007Z host5 e - id3 [custom@32473 exkey="3"] An application event log entry...`,
			}

			err = writeMessagesToStream(c, messages, tt.fmtFunc)
			require.NoError(t, err)
			require.NoError(t, c.Close())

			if tt.protocol == protocolUDP {
				time.Sleep(time.Second)
				require.NoError(t, tgt.Stop())
			} else {
				defer func() {
					require.NoError(t, tgt.Stop())
				}()
			}

			require.Eventuallyf(t, func() bool {
				return len(client.Received()) == len(messages)
			}, time.Second, 10*time.Millisecond, "Expected to receive %d messages.", len(messages))

			labels := make([]model.LabelSet, 0, len(messages))
			for _, entry := range client.Received() {
				labels = append(labels, entry.Labels)
			}
			// we only check if one of the received entries contain the wanted label set
			// because UDP does not guarantee the order of the messages
			require.Contains(t, labels, model.LabelSet{
				"test": "syslog_target",

				"severity": "notice",
				"facility": "local4",
				"hostname": "host5",
				"app_name": "e",
				"msg_id":   "id1",

				"sd_custom_exkey": "1",
			})
			require.Equal(t, "An application event log entry...", client.Received()[0].Line)

			require.NotZero(t, client.Received()[0].Timestamp)
		})
	}
}

func relabelConfig(t *testing.T) []*relabel.Config {
	relabelCfg := `
- source_labels: ['__syslog_message_severity']
  target_label: 'severity'
- source_labels: ['__syslog_message_facility']
  target_label: 'facility'
- source_labels: ['__syslog_message_hostname']
  target_label: 'hostname'
- source_labels: ['__syslog_message_app_name']
  target_label: 'app_name'
- source_labels: ['__syslog_message_proc_id']
  target_label: 'proc_id'
- source_labels: ['__syslog_message_msg_id']
  target_label: 'msg_id'
- source_labels: ['__syslog_message_sd_custom_32473_exkey']
  target_label: 'sd_custom_exkey'
`

	var relabels []*relabel.Config
	err := yaml.Unmarshal([]byte(relabelCfg), &relabels)
	require.NoError(t, err)

	return relabels
}

func writeMessagesToStream(w io.Writer, messages []string, formatter formatFunc) error {
	for _, msg := range messages {
		_, err := fmt.Fprint(w, formatter(msg))
		if err != nil {
			return err
		}
	}
	return nil
}

func TestSyslogTarget_RFC5424Messages(t *testing.T) {
	for _, tt := range []struct {
		name     string
		protocol string
		fmtFunc  formatFunc
	}{
		{"tpc newline separated", protocolTCP, fmtNewline},
		{"tpc octetcounting", protocolTCP, fmtOctetCounting},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			w := log.NewSyncWriter(os.Stderr)
			logger := log.NewLogfmtLogger(w)
			client := fake.New(func() {})

			metrics := NewMetrics(nil)
			tgt, err := NewSyslogTarget(metrics, logger, client, []*relabel.Config{}, &scrapeconfig.SyslogTargetConfig{
				ListenAddress:       "127.0.0.1:0",
				ListenProtocol:      tt.protocol,
				LabelStructuredData: true,
				Labels: model.LabelSet{
					"test": "syslog_target",
				},
				UseRFC5424Message: true,
			})
			require.NoError(t, err)
			require.Eventually(t, tgt.Ready, time.Second, 10*time.Millisecond)
			defer func() {
				require.NoError(t, tgt.Stop())
			}()

			addr := tgt.ListenAddress().String()
			c, err := net.Dial(tt.protocol, addr)
			require.NoError(t, err)

			messages := []string{
				`<165>1 2018-10-11T22:14:15.003Z host5 e - id1 [custom@32473 exkey="1"] An application event log entry...`,
				`<165>1 2018-10-11T22:14:15.005Z host5 e - id2 [custom@32473 exkey="2"] An application event log entry...`,
				`<165>1 2018-10-11T22:14:15.007Z host5 e - id3 [custom@32473 exkey="3"] An application event log entry...`,
			}

			err = writeMessagesToStream(c, messages, tt.fmtFunc)
			require.NoError(t, err)
			require.NoError(t, c.Close())

			require.Eventuallyf(t, func() bool {
				return len(client.Received()) == len(messages)
			}, time.Second, time.Millisecond, "Expected to receive %d messages, got %d.", len(messages), len(client.Received()))

			for i := range messages {
				require.Equal(t, model.LabelSet{
					"test": "syslog_target",
				}, client.Received()[i].Labels)
				require.Contains(t, messages, client.Received()[i].Line)
				require.NotZero(t, client.Received()[i].Timestamp)
			}
		})
	}
}

func TestSyslogTarget_TLSConfigWithoutServerCertificate(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := fake.New(func() {})

	metrics := NewMetrics(nil)
	_, err := NewSyslogTarget(metrics, logger, client, relabelConfig(t), &scrapeconfig.SyslogTargetConfig{
		ListenAddress: "127.0.0.1:0",
		TLSConfig: promconfig.TLSConfig{
			KeyFile: "foo",
		},
	})
	require.Error(t, err, "error setting up syslog target: certificate and key files are required")
}

func TestSyslogTarget_TLSConfigWithoutServerKey(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := fake.New(func() {})

	metrics := NewMetrics(nil)
	_, err := NewSyslogTarget(metrics, logger, client, relabelConfig(t), &scrapeconfig.SyslogTargetConfig{
		ListenAddress: "127.0.0.1:0",
		TLSConfig: promconfig.TLSConfig{
			CertFile: "foo",
		},
	})
	require.Error(t, err, "error setting up syslog target: certificate and key files are required")
}

func TestSyslogTarget_TLSConfig(t *testing.T) {
	t.Run("NewlineSeparatedMessages", func(t *testing.T) {
		testSyslogTargetWithTLS(t, fmtNewline)
	})
	t.Run("OctetCounting", func(t *testing.T) {
		testSyslogTargetWithTLS(t, fmtOctetCounting)
	})
}

func testSyslogTargetWithTLS(t *testing.T, fmtFunc formatFunc) {
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	serverCertFile, err := createTempFile(serverCert)
	if err != nil {
		t.Fatalf("Unable to create server certificate temporary file: %s", err)
	}
	defer os.Remove(serverCertFile.Name())

	serverKeyFile, err := createTempFile(serverKey)
	if err != nil {
		t.Fatalf("Unable to create server key temporary file: %s", err)
	}
	defer os.Remove(serverKeyFile.Name())

	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := fake.New(func() {})

	metrics := NewMetrics(nil)
	tgt, err := NewSyslogTarget(metrics, logger, client, relabelConfig(t), &scrapeconfig.SyslogTargetConfig{
		ListenAddress:       "127.0.0.1:0",
		LabelStructuredData: true,
		Labels: model.LabelSet{
			"test": "syslog_target",
		},
		TLSConfig: promconfig.TLSConfig{
			CertFile: serverCertFile.Name(),
			KeyFile:  serverKeyFile.Name(),
		},
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, tgt.Stop())
	}()

	tlsConfig := tls.Config{
		RootCAs:    caCertPool,
		ServerName: "promtail.example.com",
	}

	addr := tgt.ListenAddress().String()
	c, err := tls.Dial("tcp", addr, &tlsConfig)
	require.NoError(t, err)

	validMessages := []string{
		`<165>1 2018-10-11T22:14:15.003Z host5 e - id1 [custom@32473 exkey="1"] An application event log entry...`,
		`<165>1 2018-10-11T22:14:15.005Z host5 e - id2 [custom@32473 exkey="2"] An application event log entry...`,
		`<165>1 2018-10-11T22:14:15.007Z host5 e - id3 [custom@32473 exkey="3"] An application event log entry...`,
	}
	// Messages that are malformed but still valid.
	// This causes error messages being written, but the parser does not stop and close the connection.
	malformeddMessages := []string{
		`<165>1    -   An application event log entry...`,
		`<165>1 2018-10-11T22:14:15.007Z host5 e -   An application event log entry...`,
	}
	messages := append(malformeddMessages, validMessages...)

	err = writeMessagesToStream(c, messages, fmtFunc)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	require.Eventuallyf(t, func() bool {
		return len(client.Received()) == len(validMessages)
	}, time.Second, time.Millisecond, "Expected to receive %d messages, got %d.", len(validMessages), len(client.Received()))

	require.Equal(t, model.LabelSet{
		"test": "syslog_target",

		"severity": "notice",
		"facility": "local4",
		"hostname": "host5",
		"app_name": "e",
		"msg_id":   "id1",

		"sd_custom_exkey": "1",
	}, client.Received()[0].Labels)
	require.Equal(t, "An application event log entry...", client.Received()[0].Line)

	require.NotZero(t, client.Received()[0].Timestamp)
}

func createTempFile(data []byte) (*os.File, error) {
	tmpFile, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %s", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write data to temporary file: %s", err)
	}

	if err := tmpFile.Close(); err != nil {
		return nil, err
	}

	return tmpFile, nil
}

func TestSyslogTarget_TLSConfigVerifyClientCertificate(t *testing.T) {
	t.Run("NewlineSeparatedMessages", func(t *testing.T) {
		testSyslogTargetWithTLSVerifyClientCertificate(t, fmtNewline)
	})
	t.Run("OctetCounting", func(t *testing.T) {
		testSyslogTargetWithTLSVerifyClientCertificate(t, fmtOctetCounting)
	})
}

func testSyslogTargetWithTLSVerifyClientCertificate(t *testing.T, fmtFunc formatFunc) {
	caCertFile, err := createTempFile(caCert)
	if err != nil {
		t.Fatalf("Unable to create CA certificate temporary file: %s", err)
	}
	defer os.Remove(caCertFile.Name())

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	serverCertFile, err := createTempFile(serverCert)
	if err != nil {
		t.Fatalf("Unable to create server certificate temporary file: %s", err)
	}
	defer os.Remove(serverCertFile.Name())

	serverKeyFile, err := createTempFile(serverKey)
	if err != nil {
		t.Fatalf("Unable to create server key temporary file: %s", err)
	}
	defer os.Remove(serverKeyFile.Name())

	clientCertFile, err := createTempFile(clientCert)
	if err != nil {
		t.Fatalf("Unable to create client certificate temporary file: %s", err)
	}
	defer os.Remove(clientCertFile.Name())

	clientKeyFile, err := createTempFile(clientKey)
	if err != nil {
		t.Fatalf("Unable to create client key temporary file: %s", err)
	}
	defer os.Remove(clientKeyFile.Name())

	clientCerts, err := tls.LoadX509KeyPair(clientCertFile.Name(), clientKeyFile.Name())
	if err != nil {
		t.Fatalf("Unable to load client certificate or key: %s", err)
	}

	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := fake.New(func() {})

	metrics := NewMetrics(nil)
	tgt, err := NewSyslogTarget(metrics, logger, client, relabelConfig(t), &scrapeconfig.SyslogTargetConfig{
		ListenAddress:       "127.0.0.1:0",
		LabelStructuredData: true,
		Labels: model.LabelSet{
			"test": "syslog_target",
		},
		TLSConfig: promconfig.TLSConfig{
			CAFile:   caCertFile.Name(),
			CertFile: serverCertFile.Name(),
			KeyFile:  serverKeyFile.Name(),
		},
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, tgt.Stop())
	}()

	tlsConfig := tls.Config{
		RootCAs:    caCertPool,
		ServerName: "promtail.example.com",
	}

	addr := tgt.ListenAddress().String()

	t.Run("WithoutClientCertificate", func(t *testing.T) {
		c, err := tls.Dial("tcp", addr, &tlsConfig)
		require.NoError(t, err)

		err = c.SetDeadline(time.Now().Add(time.Second))
		require.NoError(t, err)

		buf := make([]byte, 1)
		_, err = c.Read(buf)
		require.EqualError(t, err, "remote error: tls: bad certificate")
	})

	t.Run("WithClientCertificate", func(t *testing.T) {
		tlsConfig.Certificates = []tls.Certificate{clientCerts}
		c, err := tls.Dial("tcp", addr, &tlsConfig)
		require.NoError(t, err)

		messages := []string{
			`<165>1 2018-10-11T22:14:15.003Z host5 e - id1 [custom@32473 exkey="1"] An application event log entry...`,
			`<165>1 2018-10-11T22:14:15.005Z host5 e - id2 [custom@32473 exkey="2"] An application event log entry...`,
			`<165>1 2018-10-11T22:14:15.007Z host5 e - id3 [custom@32473 exkey="3"] An application event log entry...`,
		}

		err = writeMessagesToStream(c, messages, fmtFunc)
		require.NoError(t, err)
		require.NoError(t, c.Close())

		require.Eventuallyf(t, func() bool {
			return len(client.Received()) == len(messages)
		}, time.Second, time.Millisecond, "Expected to receive %d messages, got %d.", len(messages), len(client.Received()))

		require.Equal(t, model.LabelSet{
			"test": "syslog_target",

			"severity": "notice",
			"facility": "local4",
			"hostname": "host5",
			"app_name": "e",
			"msg_id":   "id1",

			"sd_custom_exkey": "1",
		}, client.Received()[0].Labels)
		require.Equal(t, "An application event log entry...", client.Received()[0].Line)

		require.NotZero(t, client.Received()[0].Timestamp)
	})
}

func TestSyslogTarget_InvalidData(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := fake.New(func() {})
	metrics := NewMetrics(nil)

	tgt, err := NewSyslogTarget(metrics, logger, client, relabelConfig(t), &scrapeconfig.SyslogTargetConfig{
		ListenAddress: "127.0.0.1:0",
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, tgt.Stop())
	}()

	addr := tgt.ListenAddress().String()
	c, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer c.Close()

	_, err = fmt.Fprint(c, "xxx")
	require.NoError(t, err)

	// syslog target should immediately close the connection if sent invalid data
	err = c.SetDeadline(time.Now().Add(time.Second))
	require.NoError(t, err)

	buf := make([]byte, 1)
	_, err = c.Read(buf)
	require.EqualError(t, err, "EOF")
}

func TestSyslogTarget_NonUTF8Message(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := fake.New(func() {})
	metrics := NewMetrics(nil)

	tgt, err := NewSyslogTarget(metrics, logger, client, relabelConfig(t), &scrapeconfig.SyslogTargetConfig{
		ListenAddress: "127.0.0.1:0",
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, tgt.Stop())
	}()

	addr := tgt.ListenAddress().String()
	c, err := net.Dial("tcp", addr)
	require.NoError(t, err)

	msg1 := "Some non utf8 \xF8\xF7\xE3\xE4 characters"
	require.False(t, utf8.ValidString(msg1), "msg must no be valid utf8")
	msg2 := "\xF8 other \xF7\xE3\xE4 characters \xE3"
	require.False(t, utf8.ValidString(msg2), "msg must no be valid utf8")

	err = writeMessagesToStream(c, []string{
		"<165>1 - - - - - - " + msg1,
		"<123>1 - - - - - - " + msg2,
	}, fmtOctetCounting)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	require.Eventuallyf(t, func() bool {
		return len(client.Received()) == 2
	}, time.Second, time.Millisecond, "Expected to receive 2 messages, got %d.", len(client.Received()))

	require.Equal(t, msg1, client.Received()[0].Line)
	require.Equal(t, msg2, client.Received()[1].Line)
}

func TestSyslogTarget_IdleTimeout(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	client := fake.New(func() {})
	metrics := NewMetrics(nil)

	tgt, err := NewSyslogTarget(metrics, logger, client, relabelConfig(t), &scrapeconfig.SyslogTargetConfig{
		ListenAddress: "127.0.0.1:0",
		IdleTimeout:   time.Millisecond,
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, tgt.Stop())
	}()

	addr := tgt.ListenAddress().String()
	c, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer c.Close()

	// connection should be closed before the higher timeout
	// from SetDeadline fires
	err = c.SetDeadline(time.Now().Add(time.Second))
	require.NoError(t, err)

	buf := make([]byte, 1)
	_, err = c.Read(buf)
	require.EqualError(t, err, "EOF")
}

func TestParseStream_WithAsyncPipe(t *testing.T) {
	lines := [3]string{
		"<165>1 2018-10-11T22:14:15.003Z host5 e - id1 [custom@32473 exkey=\"1\"] An application event log entry...\n",
		"<165>1 2018-10-11T22:14:15.005Z host5 e - id2 [custom@32473 exkey=\"2\"] An application event log entry...\n",
		"<165>1 2018-10-11T22:14:15.007Z host5 e - id3 [custom@32473 exkey=\"3\"] An application event log entry...\n",
	}

	addr := &net.UDPAddr{IP: net.IP{127, 0, 0, 1}, Port: 1514}
	pipe := NewConnPipe(addr)
	go func() {
		for _, line := range lines {
			_, _ = pipe.Write([]byte(line))
		}
		pipe.Close()
	}()

	results := make([]*syslog.Result, 0)
	cb := func(res *syslog.Result) {
		results = append(results, res)
	}

	err := syslogparser.ParseStream(pipe, cb, defaultMaxMessageLength)
	require.NoError(t, err)
	require.Equal(t, 3, len(results))
}
