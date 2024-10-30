package syslog

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-kit/log"
	"github.com/leodido/go-syslog/v4"
	promconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/syslog/syslogparser"
)

var (
	caCert = []byte(`
-----BEGIN CERTIFICATE-----
MIIFDTCCAvWgAwIBAgIRAL3YFsDcKtnEWCzE0qafwlQwDQYJKoZIhvcNAQELBQAw
IDEeMBwGA1UEAxMVUHJvbXRhaWwgVGVzdCBSb290IENBMB4XDTIyMDYyOTA4MTQy
MFoXDTQyMDYyOTA4MTQyMFowIDEeMBwGA1UEAxMVUHJvbXRhaWwgVGVzdCBSb290
IENBMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA1wHnEwW3Gc1Q3v4F
BgFL9N2rayHA7yFqViEwG8AiliaCnnN5VaAN29tpWMgr9sLpve5Ka8iCO8xnIxsM
5rtlSvLlFW0SInXyJsBT6NyqHrk2GZBhscgT+Qb9ouSYjil4UFRADAAEhBZPVisO
ZQiELKPS+BxyRL15QhMB7k7u1z0GRzlrG7CcopzrYM+4JLGE+2nXnoy7MSjdNduH
w4sWwI66hD192Lpkh83HZneONXiZhJdEJOHHJ8G+rYwuZRAlnLs82y+OROIozuKV
yFhWMk+BUGgkeftmAzIiUAneKwKugqz9QExVPo1imGAsiVHdprTTPn/34ZNpsDR7
MXwzitVqvu/pa3BYDha7RTpeCdVPFqDs8BPFgcAleM75QQcnPtbUD/apJHUQ5D5q
8c2U/2hTtcTMZIMCscoBBpx98bSOS9ojIJSGYKCdBj6rqnAFNYasayVCN5c5pPIw
Mj7HUww4gKXVKMvNDnjaXqFBEgEjSv5cquZ993C8gVGHLiMnq/Bj5k9SeOD0gTKD
/uLuLhKiMI6sOJQrYW5W0P3tTNcYGUeS0hBZW2Mo6PM+BimfzstZhsdFQnjzMEld
I5elwqWKysEvxXIvLnGLVWXJ6s0Pyr6J0ASmxpjskQEcPgaOxkRVwNzC18eSq3Ey
zUEWoyHiDedB4CCIq6nXY01FC5MCAwEAAaNCMEAwDgYDVR0PAQH/BAQDAgKkMA8G
A1UdEwEB/wQFMAMBAf8wHQYDVR0OBBYEFFBj1+WwOdP8rb95R7kOmWena3RfMA0G
CSqGSIb3DQEBCwUAA4ICAQDQtOpfNPrv+V8ObOb+pie//UfTReOwXtBZgTxMu1Bi
uYIiFEveQ18HGAeIhbVm8wLjtfPkoJlZKzdCcMqSmLt0LZQNs13IQ8M+YuXeu5pX
PJOu0WrbdQHW5JMnPxwKMk5T2axbrX9dYW9gvr8sh5OlnNTx88fY1vMBsnNLVZT+
kC7Daf0zT0Go6VxBk6KRtfpoVCy/aYgwbJ1u5W61eYtXC4ezrpoLINo7zVs7Tob8
iIxJMQ/iEk/7BbIgVh9z8lNeexdjXCqm8952pPhkGONneCESTxJjgYqXyhlV0ict
OP+CiMHl5yf6+eklfXU78Jmkh/46XBqu7JV8lt8xbbSJ4AITWxi3csxC/Xztyu/g
H5nS2gAmpAbTjHq7lGicFScBuR3g0o3+64KN3XhVXN1KFrTlsGSOxyPE657p2xNw
tAAQs9IEBIPVqMInOGue6LbHTzCzW8hMClfu0CwPXiMY5DWKif7O9kzxnb9HdmAl
gnDn+9OzTw7oAW1UyVTSbzYy0Tw1ioYR7U77Z+Gcst2+mIuWp7BFNkaX5mPaacCT
GLnXoZk8UU0ph5/uBfZL3dNsJKeXlnCMAXACdLicnMC19g6P/dKqRZcI2w7kTPtF
2GlKMYjeKDuAUc9VeGzDc6PAX4oqF8XPfP5Mie4nrtpYnL0zKV1RcUS1yX1TC24O
ig==
-----END CERTIFICATE-----
`)

	// Unused, but can be useful to (re)generate some certificates
	// nolint:deadcode,unused,varcheck
	caKey = []byte(`
-----BEGIN RSA PRIVATE KEY-----
MIIJJwIBAAKCAgEA1wHnEwW3Gc1Q3v4FBgFL9N2rayHA7yFqViEwG8AiliaCnnN5
VaAN29tpWMgr9sLpve5Ka8iCO8xnIxsM5rtlSvLlFW0SInXyJsBT6NyqHrk2GZBh
scgT+Qb9ouSYjil4UFRADAAEhBZPVisOZQiELKPS+BxyRL15QhMB7k7u1z0GRzlr
G7CcopzrYM+4JLGE+2nXnoy7MSjdNduHw4sWwI66hD192Lpkh83HZneONXiZhJdE
JOHHJ8G+rYwuZRAlnLs82y+OROIozuKVyFhWMk+BUGgkeftmAzIiUAneKwKugqz9
QExVPo1imGAsiVHdprTTPn/34ZNpsDR7MXwzitVqvu/pa3BYDha7RTpeCdVPFqDs
8BPFgcAleM75QQcnPtbUD/apJHUQ5D5q8c2U/2hTtcTMZIMCscoBBpx98bSOS9oj
IJSGYKCdBj6rqnAFNYasayVCN5c5pPIwMj7HUww4gKXVKMvNDnjaXqFBEgEjSv5c
quZ993C8gVGHLiMnq/Bj5k9SeOD0gTKD/uLuLhKiMI6sOJQrYW5W0P3tTNcYGUeS
0hBZW2Mo6PM+BimfzstZhsdFQnjzMEldI5elwqWKysEvxXIvLnGLVWXJ6s0Pyr6J
0ASmxpjskQEcPgaOxkRVwNzC18eSq3EyzUEWoyHiDedB4CCIq6nXY01FC5MCAwEA
AQKCAgAeBY79cfvaJ3gWWwPajc3MWDN6VxE4ksLlWeb8yPxLWP8+HsOfeCTXQTDZ
i8HPx/GZaq+Lk0jUDruMBFft09bV+0qPjlZM54kzbgGJb151wcjTEv0BNP3M9PPv
jdnbZ+D73ne+9TWsN+1GC+cLpn/GN+3aZSZzgL1ww3SukOj6tvOseFEDYcrNTfnz
361Hul3mOSY5Zk8xExKoVYoEfORlaMiUdH2hCI3HBK3GGgWKY9eT0wdZ2wjS/VOh
qgREalfGJcLenCpSZf3qvWrKucL3bXCSCKinO7pH0fVGlcom2U4CwyLtmnsAq/9L
ZYpydjLr9y3T+UxkfA/y4bEd/Mi5ZWdM1Dd7yP5I6RgiBaPU4RF1JZZRltD9tCc7
D2Qw8nKCHRf455+KGxMX7f3MdJEiV0O4TW5aU5QPxKhuYcum0iMgQqTiIj+rTcTK
+nuzFcU0GRIDI2n7pW79wWMCA0mz1RQ7CSLtsgaH9gOCnetYik7Tj23FOi7NnBtW
D0D30A3ZhdNy0Khjz4E67l1yRjzON4VO/j2zkeD39kUNpTXk+ry1Igy6EpWsGNc9
AVycaUGqF75VWTpR8zi/qV+m9aMORM5fyviBu+vxTfDl1bzthGDWy7lJEPeC76wl
Fs8byuK8BJT4jtrCQ6bUqeSgks8O3jNFjnrZLUo3HC2f0cA24QKCAQEA3SNlWlw5
a2sm6bg9F3BKorG2GZdNDi8ENWBzKpQFFaDaryfeToHHwRpr4qMEsLSMSAAxbpHc
vUDKgLvlwhrOBDZKe+KmgHH7bODzgs1x2an4KCjhBD+YJBKaL7O5bJ9o019fcWie
DQvNoaNayl6xz3juDl8tW9l8FOfYxv3MMT1mShH6hd62OhbVPSVtNUPXKZg/Apz7
CrsMBWDwRErH879U/IjLvOWfI0lCeNekO9fMdtKrczhCzfNmclSSZqoeRiOj/ZGo
ZuSGgSQJF7ZyfnZ5j4gfCfyTnijW7SuKOTImVU6h2W3qlv3nWcVKg/BQwrj4eHoM
5WjFtj+jEGIRkQKCAQEA+OcUx2wg+BU3wu3Z9LfL34wcRKgoiwpuvT0OofuI6bwz
GQoM6K4KsNoacZvv2Bj2QydM6fmF5mvl+q60hvbxSJ0Xugi6jqwIZX/n5XKyZ9qO
2ls/5izdETjNCT3okrdlUYxkDhI3Eqx/A5kiStBnJDoone4V834FnTXBVquJzxFP
JG63qpcGGko7Fx/xY9Y/ZvCjwtC4qr34DOwIcT+Jtci6CHZeaSr7Jq/KC69ujJ8n
3IByqGepNbVEHZsXYTYrKRXWzTuQw9owmcJOqkK2dYe+cEUsUHLBVvCwgv7swnb6
3zG5KR4CE19aTCcVmIplzqMlVFxavQepH2jAuxv44wKCAQAe0uM6wCYkye/Hni2t
ybItkVXPpV5RPs54XjRPWAiJZj11Mrpy+PYN/Y/SLGTn+JKhKp25Ss2Y96ICZa51
6uSSg7rIH+STfM/N8mEe92IKM/3qIyCSRgb/6DPjuEp9UI78/4s/NJTrPpzwDeQG
10IzqCiOike5SMxZ4aM+wXun1WYfpvfjlxKRcENS3ZemWAlyu8z0oUsAyOe5DDUR
X9cVK7M97BdyAhO3iGuiinRS/xZ57Y2GZu4w5N9/yjgJ5WaI4kjmfFob1XjGIW6/
BmhZJkx1bETfUHyHDCxBLNN8e3gKZgZ7Vy3e1A9eXPixAVtQeRXxPRn1FDCS4bXp
/7FxAoIBADSHWCxKFp8kozMBTXlG/MC96g1XS88kMYDAjQEEe72QWVxUcar9aAYw
0Vnepfx+MCK1/ZZ3cZnSdaO1ESZWoU9I0AQT6YNIrTD2kHMtBJfEWVed4FtsZm9H
BIaJyTaFe918+nS5xWOsgdW5kLInT00m9QF3iKxtkTO/b4EiDKBlr8UplJts6f3M
YrIbrK78PT81U+o+cGqgUuQvQAzecuqpZRF6IayiRITCnqpeqL8Gq7vuY8REtEJA
chKpc4KxkuRF1qJTita6in04s69dCvK85iT9hD+qKEF35FiRAlh8Ea/e54vU6G08
N2tQ6E7cDmZQqgUmxIOWRUv6qIoUei8CggEAGs6QILgxqN+7MLBn5Gqpur7Pe8ZC
8aiLUDH/1OsXPQGTZ+N4AA1oe4GSJO7DqZqTp7Zy35oedNZ3uFOXCJ0aU+Q+hfey
gp4QIipYYzC5AFCwOkxDvF5CEL3Ri+3HJM7MzEPYzGOBlxngzEtlV4RmnoaUOCZi
N/trZVF/IEB8J/XrHvWUykS8l/3bTTEsDfm7zgGsLaviF7eSHXTBUozZrRYJH+e8
2A3qDtgaYa0ZR7c4kVoO2tRQeJYLCVowMWZ42OsFJAKoTLB0wGBqgrDthsaOk1l7
hN4Ta/OHuRuUu49nu9cn8T9zQm5viulglf5saeEumoahPLFQDwJCk67Yxw==
-----END RSA PRIVATE KEY-----
`)

	serverCert = []byte(`
-----BEGIN CERTIFICATE-----
MIIFWDCCA0CgAwIBAgIRAMWnlYEx52Vws2b3EzexW+UwDQYJKoZIhvcNAQELBQAw
IDEeMBwGA1UEAxMVUHJvbXRhaWwgVGVzdCBSb290IENBMB4XDTIyMDYyOTA4MTQy
NFoXDTQyMDYyOTA4MTQyNFowKzEpMCcGA1UEAxMgUHJvbXRhaWwgVGVzdCBTZXJ2
ZXIgQ2VydGlmaWNhdGUwggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQCo
8+m8sVjG2tvRqzFWD6XRwvWIV/7nOHhuL7ygdouTJ5MKjpKHSudOXOoxHw9aNA4u
zmExqlSD4RVnwb2R/nXoqO1Ae5VavkNngeDw5Uk/RacqF4WFCnmFNxh523iA4ifo
ZxmVhQoE1t3O4kMrygK2SEpSTY58PfuqLfd7ZyHDEmpu9pYfuQQCqKLOsVQS5mqY
g7gyKQQLq0D4beCsSuih09PskMiG29uh/qCdSmDDR++j42xP/fwXVGm3nsOMLjQf
1DFlZFqmQS42Da1KtnKzz+SVDSHihyRKiZ0KAyyDHYdKToFO/zYlCgFGw86o4VGu
HKbolE6oPrBnqdXyIf9cON4RdnoTrChG4E6yq4LN8XoNRloePjoGQ9huoqwGie3m
c06Pnj2eV4oCJwdLarKZqZzdJnanx+ctObAGGGjjV1ocNGFc2EMBpTFVpy20Kqd/
E7t2yeGU6wJhzMNtvyFHhcOY+7PRTuvrUzbK1McFU/6NKBn4ioW2XBU8CjPEKGNx
itIZWnxppqkt2EemZEB+L1QpNtNlFmSYLqPm+j1134os3I3s+kkawiSjBcpqgzDf
stt64LiO/ULXDfPexbzE+mggQ3GsQ+b5v2O4KTQ4A4ilI48+0Qn3YxqfaVJmusGE
+Jkpo/Fnt3OCJaYBqsSzK5dIzF9TJhsOb+f5NGBJEQIDAQABo4GBMH8wDgYDVR0P
AQH/BAQDAgWgMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAMBgNVHRMB
Af8EAjAAMB8GA1UdIwQYMBaAFFBj1+WwOdP8rb95R7kOmWena3RfMB8GA1UdEQQY
MBaCFHByb210YWlsLmV4YW1wbGUuY29tMA0GCSqGSIb3DQEBCwUAA4ICAQApSDHl
IECz4Ix+6zZqNU0DgulYTXcHKtnXEKRJVTULbKy/mWzE5ZAgVV0rpRLHbRg0fWYh
4tjd5X/Pedf4OtnpV6ekkvBf+QCKAmDiWN9rKhjdVoTFw0+wavQv1yzz8++2HenR
rgq7wWuwg0hB9/KKOWW09ZuBynjB7aeoWHztIbD+/cugMlzqDX8hODRl3OaEAwM3
PBpYEqFSuPBNrCK5Y0tGgzZDdsxrDYcX5CjJdrgwtZ1kFOT8J2VE+2Sqc7KMCPFl
LQmmtIcwjljNABf5d2L978tISFtwRoZVxrsTODqLLF8l6B7JUQdCpmoM+549E0cS
bpTj4FVftJwEpElMFMVw/Uh5pA7hOLGAK3YYqBx6fbEGHn+WmwLA5Io7tbBSKUX/
hNlOFupAU3Q/OawqlmSPRflqKIg4b8TgSVlafJOFmvKg0WLGVuLtl65g9eNQVht9
KLlPPCPbpYKXpXya42eFazxEepOeaCkchOUB+x4tmAZ44NJTMR+UmiGLGCjdQTpU
m892bbGtLA0hGhYGrMsI546Xd8PjNI117CARrDpk57E1V+CsAO6ZTOPLgI4q976L
kWYB12V1t/BNtGyChdmfIYBqvTYAGk9hbqG80SjmcZtPu5JIEvIXCyaYXiWmEs2l
j1ieRNjgNYyG/7wJmilCsnpjbWza85/JzxE1cA==
-----END CERTIFICATE-----
`)

	serverKey = []byte(`
-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----
`)

	clientCert = []byte(`
-----BEGIN CERTIFICATE-----
MIIFNTCCAx2gAwIBAgIQWzE3b7oUqYGe2UvYWJBnyDANBgkqhkiG9w0BAQsFADAg
MR4wHAYDVQQDExVQcm9tdGFpbCBUZXN0IFJvb3QgQ0EwHhcNMjIwNjI5MDgxNDI0
WhcNNDIwNjI5MDgxNDI0WjArMSkwJwYDVQQDEyBQcm9tdGFpbCBUZXN0IENsaWVu
dCBDZXJ0aWZpY2F0ZTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBALcQ
KhIIL7EZLIDWPMIkT5cJdPRavs5/LO50isCVs2z+PTA4mIkj2NEk8qFqgc3+1LUh
SV/NQ3p0LikSx9M6g1oBJbqQ7W6BcCzzUOHb4QlGoa2DXXGONPdO3AJxTTDNU6Li
xT5ESTbuDe56JDw3x2V0pxOzCUSuTMa1mvHnIojVn/8qHOZB0l4o9c44oHGJDUGh
o1wAc3UXGMLsd6IEYsHzLyG6kG7ayqToFftI/rkpEK8tCWMbclewh22c0OCXejEZ
pS9mz2decoIG7NnkqrRwUj+RPNlKJgBJ2USYimbQZHmdZWNb+vP/HFXs1Eq0sin+
5HrRnzuB6yCwQlqvR7Db3buENEXG2OcglGmO/1mBfB4nW7JKFAvIqBBtHDRmBCYr
YS9htPiZte8Mfak3RncxLLccEzo1CNqtW9BLfryGH3l+6LR0yZkw+C95RZ9Wni4T
e5hy5yX8SW3z4AUb3ZYbFROW4sW/sSsh8VucZoa4OXw981OB4ZGrtJnncvc1KE5Q
BBF+bRZKdMQhzwb7pZq31kZjH0So53Ntyf4S6VHgUOTs8xorPSjnP85BKnnm7XsG
W/zP+kGCyjOCu4WIrNvSlIguF4KcVZulWbMljVCU0TwKNbxBm5SZrTXPoJeyRtE6
ai+fHPoMjJvwUaLYQUkzTpfdGg8OCV3NX/LvbCuVAgMBAAGjYDBeMA4GA1UdDwEB
/wQEAwIFoDAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDAYDVR0TAQH/
BAIwADAfBgNVHSMEGDAWgBRQY9flsDnT/K2/eUe5Dplnp2t0XzANBgkqhkiG9w0B
AQsFAAOCAgEAxJvDifKqK6EQrN5VPL68IPORu1VK17HeSJ2xLScL3Hh7vulPs9Nf
HLixcOhixsYhgurFZ3M3K05tT0EKy1K7WLJzJEhPccAkbz+d2oIeghksq2l75u20
t2o1X8rgT+/7d/j+VBur/+igQdwvylo+wgGNosX8VmjyQrZBWIHvDTyzrvODFbZa
JYf3DfgLSF5tr4e/HNLhynUD9G40CmRLQh7PkojrMXMyeqqWpBPzDBlZgvH2QMK8
K9S8KUuaZhGUTLQmkR3NP/bx1V/Ks1/BtpmdIchQ42w+Uu2PM/pmuZsnX7bK0tzd
zCFjgSIxifN8BKdcKMEngo+MwYy/Fj3vzy4qXxtOCbnzB+A/ziLiJV3Tsdwm+nPO
xZFcXXfnCMoNF0ouv2/WAbUrKLTZXL712MZ14JT79TkKxWZ49AUHSiGfm1I45iky
xgcn4FVgJpsCheRqL+gecNDyq+4VdwlWuFAuqMI4UBUbWwyAntOL1iiyENGA1ygo
OVuQq9M0bh93d5U+Ct79CsL4LLaoRrvBGJ6WnO8PKTTLqfFC3T8ySCtVGZiLoSqn
fJ1uhxhK8YSYhod+81/nkpJuF1xRg1t2kXgvUxPR9jzf96QaZ/94oryEH3zr59Rb
wmOulo1MN6yRdGzWiJ8sZ8VXUuh0xBCUiwrpo++Dda2s0bF0YzJGI6E=
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
		{"tcp newline separated", protocolTCP, fmtNewline},
		{"tcp octetcounting", protocolTCP, fmtOctetCounting},
		{"udp newline separated", protocolUDP, fmtNewline},
		{"udp octetcounting", protocolUDP, fmtOctetCounting},
	} {
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
		{"tcp newline separated", protocolTCP, fmtNewline},
		{"tcp octetcounting", protocolTCP, fmtOctetCounting},
	} {
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
	tmpFile, err := os.CreateTemp("", "")
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
		require.EqualError(t, err, badCertificateErrorMessage)
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

	err := syslogparser.ParseStream(false, pipe, cb, defaultMaxMessageLength)
	require.NoError(t, err)
	require.Equal(t, 3, len(results))
}
