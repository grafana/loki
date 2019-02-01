package dns

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ed25519"
)

func getKey() *DNSKEY {
	key := new(DNSKEY)
	key.Hdr.Name = "miek.nl."
	key.Hdr.Class = ClassINET
	key.Hdr.Ttl = 14400
	key.Flags = 256
	key.Protocol = 3
	key.Algorithm = RSASHA256
	key.PublicKey = "AwEAAcNEU67LJI5GEgF9QLNqLO1SMq1EdoQ6E9f85ha0k0ewQGCblyW2836GiVsm6k8Kr5ECIoMJ6fZWf3CQSQ9ycWfTyOHfmI3eQ/1Covhb2y4bAmL/07PhrL7ozWBW3wBfM335Ft9xjtXHPy7ztCbV9qZ4TVDTW/Iyg0PiwgoXVesz"
	return key
}

func getSoa() *SOA {
	soa := new(SOA)
	soa.Hdr = RR_Header{"miek.nl.", TypeSOA, ClassINET, 14400, 0}
	soa.Ns = "open.nlnetlabs.nl."
	soa.Mbox = "miekg.atoom.net."
	soa.Serial = 1293945905
	soa.Refresh = 14400
	soa.Retry = 3600
	soa.Expire = 604800
	soa.Minttl = 86400
	return soa
}

func TestSecure(t *testing.T) {
	soa := getSoa()

	sig := new(RRSIG)
	sig.Hdr = RR_Header{"miek.nl.", TypeRRSIG, ClassINET, 14400, 0}
	sig.TypeCovered = TypeSOA
	sig.Algorithm = RSASHA256
	sig.Labels = 2
	sig.Expiration = 1296534305 // date -u '+%s' -d"2011-02-01 04:25:05"
	sig.Inception = 1293942305  // date -u '+%s' -d"2011-01-02 04:25:05"
	sig.OrigTtl = 14400
	sig.KeyTag = 12051
	sig.SignerName = "miek.nl."
	sig.Signature = "oMCbslaAVIp/8kVtLSms3tDABpcPRUgHLrOR48OOplkYo+8TeEGWwkSwaz/MRo2fB4FxW0qj/hTlIjUGuACSd+b1wKdH5GvzRJc2pFmxtCbm55ygAh4EUL0F6U5cKtGJGSXxxg6UFCQ0doJCmiGFa78LolaUOXImJrk6AFrGa0M="

	key := new(DNSKEY)
	key.Hdr.Name = "miek.nl."
	key.Hdr.Class = ClassINET
	key.Hdr.Ttl = 14400
	key.Flags = 256
	key.Protocol = 3
	key.Algorithm = RSASHA256
	key.PublicKey = "AwEAAcNEU67LJI5GEgF9QLNqLO1SMq1EdoQ6E9f85ha0k0ewQGCblyW2836GiVsm6k8Kr5ECIoMJ6fZWf3CQSQ9ycWfTyOHfmI3eQ/1Covhb2y4bAmL/07PhrL7ozWBW3wBfM335Ft9xjtXHPy7ztCbV9qZ4TVDTW/Iyg0PiwgoXVesz"

	// It should validate. Period is checked separately, so this will keep on working
	if sig.Verify(key, []RR{soa}) != nil {
		t.Error("failure to validate")
	}
}

func TestSignature(t *testing.T) {
	sig := new(RRSIG)
	sig.Hdr.Name = "miek.nl."
	sig.Hdr.Class = ClassINET
	sig.Hdr.Ttl = 3600
	sig.TypeCovered = TypeDNSKEY
	sig.Algorithm = RSASHA1
	sig.Labels = 2
	sig.OrigTtl = 4000
	sig.Expiration = 1000 //Thu Jan  1 02:06:40 CET 1970
	sig.Inception = 800   //Thu Jan  1 01:13:20 CET 1970
	sig.KeyTag = 34641
	sig.SignerName = "miek.nl."
	sig.Signature = "AwEAAaHIwpx3w4VHKi6i1LHnTaWeHCL154Jug0Rtc9ji5qwPXpBo6A5sRv7cSsPQKPIwxLpyCrbJ4mr2L0EPOdvP6z6YfljK2ZmTbogU9aSU2fiq/4wjxbdkLyoDVgtO+JsxNN4bjr4WcWhsmk1Hg93FV9ZpkWb0Tbad8DFqNDzr//kZ"

	// Should not be valid
	if sig.ValidityPeriod(time.Now()) {
		t.Error("should not be valid")
	}

	sig.Inception = 315565800   //Tue Jan  1 10:10:00 CET 1980
	sig.Expiration = 4102477800 //Fri Jan  1 10:10:00 CET 2100
	if !sig.ValidityPeriod(time.Now()) {
		t.Error("should be valid")
	}
}

func TestSignVerify(t *testing.T) {
	// The record we want to sign
	soa := new(SOA)
	soa.Hdr = RR_Header{"miek.nl.", TypeSOA, ClassINET, 14400, 0}
	soa.Ns = "open.nlnetlabs.nl."
	soa.Mbox = "miekg.atoom.net."
	soa.Serial = 1293945905
	soa.Refresh = 14400
	soa.Retry = 3600
	soa.Expire = 604800
	soa.Minttl = 86400

	soa1 := new(SOA)
	soa1.Hdr = RR_Header{"*.miek.nl.", TypeSOA, ClassINET, 14400, 0}
	soa1.Ns = "open.nlnetlabs.nl."
	soa1.Mbox = "miekg.atoom.net."
	soa1.Serial = 1293945905
	soa1.Refresh = 14400
	soa1.Retry = 3600
	soa1.Expire = 604800
	soa1.Minttl = 86400

	srv := new(SRV)
	srv.Hdr = RR_Header{"srv.miek.nl.", TypeSRV, ClassINET, 14400, 0}
	srv.Port = 1000
	srv.Weight = 800
	srv.Target = "web1.miek.nl."

	hinfo := &HINFO{
		Hdr: RR_Header{
			Name:   "miek.nl.",
			Rrtype: TypeHINFO,
			Class:  ClassINET,
			Ttl:    3789,
		},
		Cpu: "X",
		Os:  "Y",
	}

	// With this key
	key := new(DNSKEY)
	key.Hdr.Rrtype = TypeDNSKEY
	key.Hdr.Name = "miek.nl."
	key.Hdr.Class = ClassINET
	key.Hdr.Ttl = 14400
	key.Flags = 256
	key.Protocol = 3
	key.Algorithm = RSASHA256
	privkey, _ := key.Generate(512)

	// Fill in the values of the Sig, before signing
	sig := new(RRSIG)
	sig.Hdr = RR_Header{"miek.nl.", TypeRRSIG, ClassINET, 14400, 0}
	sig.TypeCovered = soa.Hdr.Rrtype
	sig.Labels = uint8(CountLabel(soa.Hdr.Name)) // works for all 3
	sig.OrigTtl = soa.Hdr.Ttl
	sig.Expiration = 1296534305 // date -u '+%s' -d"2011-02-01 04:25:05"
	sig.Inception = 1293942305  // date -u '+%s' -d"2011-01-02 04:25:05"
	sig.KeyTag = key.KeyTag()   // Get the keyfrom the Key
	sig.SignerName = key.Hdr.Name
	sig.Algorithm = RSASHA256

	for _, r := range []RR{soa, soa1, srv, hinfo} {
		if err := sig.Sign(privkey.(*rsa.PrivateKey), []RR{r}); err != nil {
			t.Error("failure to sign the record:", err)
			continue
		}
		if err := sig.Verify(key, []RR{r}); err != nil {
			t.Errorf("failure to validate: %s", r.Header().Name)
			continue
		}
	}
}

func Test65534(t *testing.T) {
	t6 := new(RFC3597)
	t6.Hdr = RR_Header{"miek.nl.", 65534, ClassINET, 14400, 0}
	t6.Rdata = "505D870001"
	key := new(DNSKEY)
	key.Hdr.Name = "miek.nl."
	key.Hdr.Rrtype = TypeDNSKEY
	key.Hdr.Class = ClassINET
	key.Hdr.Ttl = 14400
	key.Flags = 256
	key.Protocol = 3
	key.Algorithm = RSASHA256
	privkey, _ := key.Generate(1024)

	sig := new(RRSIG)
	sig.Hdr = RR_Header{"miek.nl.", TypeRRSIG, ClassINET, 14400, 0}
	sig.TypeCovered = t6.Hdr.Rrtype
	sig.Labels = uint8(CountLabel(t6.Hdr.Name))
	sig.OrigTtl = t6.Hdr.Ttl
	sig.Expiration = 1296534305 // date -u '+%s' -d"2011-02-01 04:25:05"
	sig.Inception = 1293942305  // date -u '+%s' -d"2011-01-02 04:25:05"
	sig.KeyTag = key.KeyTag()
	sig.SignerName = key.Hdr.Name
	sig.Algorithm = RSASHA256
	if err := sig.Sign(privkey.(*rsa.PrivateKey), []RR{t6}); err != nil {
		t.Error(err)
		t.Error("failure to sign the TYPE65534 record")
	}
	if err := sig.Verify(key, []RR{t6}); err != nil {
		t.Error(err)
		t.Errorf("failure to validate %s", t6.Header().Name)
	}
}

func TestDnskey(t *testing.T) {
	pubkey, err := ReadRR(strings.NewReader(`
miek.nl.	IN	DNSKEY	256 3 10 AwEAAZuMCu2FdugHkTrXYgl5qixvcDw1aDDlvL46/xJKbHBAHY16fNUb2b65cwko2Js/aJxUYJbZk5dwCDZxYfrfbZVtDPQuc3o8QaChVxC7/JYz2AHc9qHvqQ1j4VrH71RWINlQo6VYjzN/BGpMhOZoZOEwzp1HfsOE3lNYcoWU1smL ;{id = 5240 (zsk), size = 1024b}
`), "Kmiek.nl.+010+05240.key")
	if err != nil {
		t.Fatal(err)
	}
	privStr := `Private-key-format: v1.3
Algorithm: 10 (RSASHA512)
Modulus: m4wK7YV26AeROtdiCXmqLG9wPDVoMOW8vjr/EkpscEAdjXp81RvZvrlzCSjYmz9onFRgltmTl3AINnFh+t9tlW0M9C5zejxBoKFXELv8ljPYAdz2oe+pDWPhWsfvVFYg2VCjpViPM38EakyE5mhk4TDOnUd+w4TeU1hyhZTWyYs=
PublicExponent: AQAB
PrivateExponent: UfCoIQ/Z38l8vB6SSqOI/feGjHEl/fxIPX4euKf0D/32k30fHbSaNFrFOuIFmWMB3LimWVEs6u3dpbB9CQeCVg7hwU5puG7OtuiZJgDAhNeOnxvo5btp4XzPZrJSxR4WNQnwIiYWbl0aFlL1VGgHC/3By89ENZyWaZcMLW4KGWE=
Prime1: yxwC6ogAu8aVcDx2wg1V0b5M5P6jP8qkRFVMxWNTw60Vkn+ECvw6YAZZBHZPaMyRYZLzPgUlyYRd0cjupy4+fQ==
Prime2: xA1bF8M0RTIQ6+A11AoVG6GIR/aPGg5sogRkIZ7ID/sF6g9HMVU/CM2TqVEBJLRPp73cv6ZeC3bcqOCqZhz+pw==
Exponent1: xzkblyZ96bGYxTVZm2/vHMOXswod4KWIyMoOepK6B/ZPcZoIT6omLCgtypWtwHLfqyCz3MK51Nc0G2EGzg8rFQ==
Exponent2: Pu5+mCEb7T5F+kFNZhQadHUklt0JUHbi3hsEvVoHpEGSw3BGDQrtIflDde0/rbWHgDPM4WQY+hscd8UuTXrvLw==
Coefficient: UuRoNqe7YHnKmQzE6iDWKTMIWTuoqqrFAmXPmKQnC+Y+BQzOVEHUo9bXdDnoI9hzXP1gf8zENMYwYLeWpuYlFQ==
`
	privkey, err := pubkey.(*DNSKEY).ReadPrivateKey(strings.NewReader(privStr),
		"Kmiek.nl.+010+05240.private")
	if err != nil {
		t.Fatal(err)
	}
	if pubkey.(*DNSKEY).PublicKey != "AwEAAZuMCu2FdugHkTrXYgl5qixvcDw1aDDlvL46/xJKbHBAHY16fNUb2b65cwko2Js/aJxUYJbZk5dwCDZxYfrfbZVtDPQuc3o8QaChVxC7/JYz2AHc9qHvqQ1j4VrH71RWINlQo6VYjzN/BGpMhOZoZOEwzp1HfsOE3lNYcoWU1smL" {
		t.Error("pubkey is not what we've read")
	}
	if pubkey.(*DNSKEY).PrivateKeyString(privkey) != privStr {
		t.Error("privkey is not what we've read")
		t.Errorf("%v", pubkey.(*DNSKEY).PrivateKeyString(privkey))
	}
}

func TestTag(t *testing.T) {
	key := new(DNSKEY)
	key.Hdr.Name = "miek.nl."
	key.Hdr.Rrtype = TypeDNSKEY
	key.Hdr.Class = ClassINET
	key.Hdr.Ttl = 3600
	key.Flags = 256
	key.Protocol = 3
	key.Algorithm = RSASHA256
	key.PublicKey = "AwEAAcNEU67LJI5GEgF9QLNqLO1SMq1EdoQ6E9f85ha0k0ewQGCblyW2836GiVsm6k8Kr5ECIoMJ6fZWf3CQSQ9ycWfTyOHfmI3eQ/1Covhb2y4bAmL/07PhrL7ozWBW3wBfM335Ft9xjtXHPy7ztCbV9qZ4TVDTW/Iyg0PiwgoXVesz"

	tag := key.KeyTag()
	if tag != 12051 {
		t.Errorf("wrong key tag: %d for key %v", tag, key)
	}
}

func TestKeyRSA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	key := new(DNSKEY)
	key.Hdr.Name = "miek.nl."
	key.Hdr.Rrtype = TypeDNSKEY
	key.Hdr.Class = ClassINET
	key.Hdr.Ttl = 3600
	key.Flags = 256
	key.Protocol = 3
	key.Algorithm = RSASHA256
	priv, _ := key.Generate(2048)

	soa := new(SOA)
	soa.Hdr = RR_Header{"miek.nl.", TypeSOA, ClassINET, 14400, 0}
	soa.Ns = "open.nlnetlabs.nl."
	soa.Mbox = "miekg.atoom.net."
	soa.Serial = 1293945905
	soa.Refresh = 14400
	soa.Retry = 3600
	soa.Expire = 604800
	soa.Minttl = 86400

	sig := new(RRSIG)
	sig.Hdr = RR_Header{"miek.nl.", TypeRRSIG, ClassINET, 14400, 0}
	sig.TypeCovered = TypeSOA
	sig.Algorithm = RSASHA256
	sig.Labels = 2
	sig.Expiration = 1296534305 // date -u '+%s' -d"2011-02-01 04:25:05"
	sig.Inception = 1293942305  // date -u '+%s' -d"2011-01-02 04:25:05"
	sig.OrigTtl = soa.Hdr.Ttl
	sig.KeyTag = key.KeyTag()
	sig.SignerName = key.Hdr.Name

	if err := sig.Sign(priv.(*rsa.PrivateKey), []RR{soa}); err != nil {
		t.Error("failed to sign")
		return
	}
	if err := sig.Verify(key, []RR{soa}); err != nil {
		t.Error("failed to verify")
	}
}

func TestKeyToDS(t *testing.T) {
	key := new(DNSKEY)
	key.Hdr.Name = "miek.nl."
	key.Hdr.Rrtype = TypeDNSKEY
	key.Hdr.Class = ClassINET
	key.Hdr.Ttl = 3600
	key.Flags = 256
	key.Protocol = 3
	key.Algorithm = RSASHA256
	key.PublicKey = "AwEAAcNEU67LJI5GEgF9QLNqLO1SMq1EdoQ6E9f85ha0k0ewQGCblyW2836GiVsm6k8Kr5ECIoMJ6fZWf3CQSQ9ycWfTyOHfmI3eQ/1Covhb2y4bAmL/07PhrL7ozWBW3wBfM335Ft9xjtXHPy7ztCbV9qZ4TVDTW/Iyg0PiwgoXVesz"

	ds := key.ToDS(SHA1)
	if strings.ToUpper(ds.Digest) != "B5121BDB5B8D86D0CC5FFAFBAAABE26C3E20BAC1" {
		t.Errorf("wrong DS digest for SHA1\n%v", ds)
	}
}

func TestSignRSA(t *testing.T) {
	pub := "miek.nl. IN DNSKEY 256 3 5 AwEAAb+8lGNCxJgLS8rYVer6EnHVuIkQDghdjdtewDzU3G5R7PbMbKVRvH2Ma7pQyYceoaqWZQirSj72euPWfPxQnMy9ucCylA+FuH9cSjIcPf4PqJfdupHk9X6EBYjxrCLY4p1/yBwgyBIRJtZtAqM3ceAH2WovEJD6rTtOuHo5AluJ"

	priv := `Private-key-format: v1.3
Algorithm: 5 (RSASHA1)
Modulus: v7yUY0LEmAtLythV6voScdW4iRAOCF2N217APNTcblHs9sxspVG8fYxrulDJhx6hqpZlCKtKPvZ649Z8/FCczL25wLKUD4W4f1xKMhw9/g+ol926keT1foQFiPGsItjinX/IHCDIEhEm1m0Cozdx4AfZai8QkPqtO064ejkCW4k=
PublicExponent: AQAB
PrivateExponent: YPwEmwjk5HuiROKU4xzHQ6l1hG8Iiha4cKRG3P5W2b66/EN/GUh07ZSf0UiYB67o257jUDVEgwCuPJz776zfApcCB4oGV+YDyEu7Hp/rL8KcSN0la0k2r9scKwxTp4BTJT23zyBFXsV/1wRDK1A5NxsHPDMYi2SoK63Enm/1ptk=
Prime1: /wjOG+fD0ybNoSRn7nQ79udGeR1b0YhUA5mNjDx/x2fxtIXzygYk0Rhx9QFfDy6LOBvz92gbNQlzCLz3DJt5hw==
Prime2: wHZsJ8OGhkp5p3mrJFZXMDc2mbYusDVTA+t+iRPdS797Tj0pjvU2HN4vTnTj8KBQp6hmnY7dLp9Y1qserySGbw==
Exponent1: N0A7FsSRIg+IAN8YPQqlawoTtG1t1OkJ+nWrurPootScApX6iMvn8fyvw3p2k51rv84efnzpWAYiC8SUaQDNxQ==
Exponent2: SvuYRaGyvo0zemE3oS+WRm2scxR8eiA8WJGeOc+obwOKCcBgeZblXzfdHGcEC1KaOcetOwNW/vwMA46lpLzJNw==
Coefficient: 8+7ZN/JgByqv0NfULiFKTjtyegUcijRuyij7yNxYbCBneDvZGxJwKNi4YYXWx743pcAj4Oi4Oh86gcmxLs+hGw==
Created: 20110302104537
Publish: 20110302104537
Activate: 20110302104537`

	xk := testRR(pub)
	k := xk.(*DNSKEY)
	p, err := k.NewPrivateKey(priv)
	if err != nil {
		t.Error(err)
	}
	switch priv := p.(type) {
	case *rsa.PrivateKey:
		if 65537 != priv.PublicKey.E {
			t.Error("exponenent should be 65537")
		}
	default:
		t.Errorf("we should have read an RSA key: %v", priv)
	}
	if k.KeyTag() != 37350 {
		t.Errorf("keytag should be 37350, got %d %v", k.KeyTag(), k)
	}

	soa := new(SOA)
	soa.Hdr = RR_Header{"miek.nl.", TypeSOA, ClassINET, 14400, 0}
	soa.Ns = "open.nlnetlabs.nl."
	soa.Mbox = "miekg.atoom.net."
	soa.Serial = 1293945905
	soa.Refresh = 14400
	soa.Retry = 3600
	soa.Expire = 604800
	soa.Minttl = 86400

	sig := new(RRSIG)
	sig.Hdr = RR_Header{"miek.nl.", TypeRRSIG, ClassINET, 14400, 0}
	sig.Expiration = 1296534305 // date -u '+%s' -d"2011-02-01 04:25:05"
	sig.Inception = 1293942305  // date -u '+%s' -d"2011-01-02 04:25:05"
	sig.KeyTag = k.KeyTag()
	sig.SignerName = k.Hdr.Name
	sig.Algorithm = k.Algorithm

	sig.Sign(p.(*rsa.PrivateKey), []RR{soa})
	if sig.Signature != "D5zsobpQcmMmYsUMLxCVEtgAdCvTu8V/IEeP4EyLBjqPJmjt96bwM9kqihsccofA5LIJ7DN91qkCORjWSTwNhzCv7bMyr2o5vBZElrlpnRzlvsFIoAZCD9xg6ZY7ZyzUJmU6IcTwG4v3xEYajcpbJJiyaw/RqR90MuRdKPiBzSo=" {
		t.Errorf("signature is not correct: %v", sig)
	}
}

func TestSignVerifyECDSA(t *testing.T) {
	pub := `example.net. 3600 IN DNSKEY 257 3 14 (
	xKYaNhWdGOfJ+nPrL8/arkwf2EY3MDJ+SErKivBVSum1
	w/egsXvSADtNJhyem5RCOpgQ6K8X1DRSEkrbYQ+OB+v8
	/uX45NBwY8rp65F6Glur8I/mlVNgF6W/qTI37m40 )`
	priv := `Private-key-format: v1.2
Algorithm: 14 (ECDSAP384SHA384)
PrivateKey: WURgWHCcYIYUPWgeLmiPY2DJJk02vgrmTfitxgqcL4vwW7BOrbawVmVe0d9V94SR`

	eckey := testRR(pub)
	privkey, err := eckey.(*DNSKEY).NewPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	// TODO: Create separate test for this
	ds := eckey.(*DNSKEY).ToDS(SHA384)
	if ds.KeyTag != 10771 {
		t.Fatal("wrong keytag on DS")
	}
	if ds.Digest != "72d7b62976ce06438e9c0bf319013cf801f09ecc84b8d7e9495f27e305c6a9b0563a9b5f4d288405c3008a946df983d6" {
		t.Fatal("wrong DS Digest")
	}
	a := testRR("www.example.net. 3600 IN A 192.0.2.1")
	sig := new(RRSIG)
	sig.Hdr = RR_Header{"example.net.", TypeRRSIG, ClassINET, 14400, 0}
	sig.Expiration, _ = StringToTime("20100909102025")
	sig.Inception, _ = StringToTime("20100812102025")
	sig.KeyTag = eckey.(*DNSKEY).KeyTag()
	sig.SignerName = eckey.(*DNSKEY).Hdr.Name
	sig.Algorithm = eckey.(*DNSKEY).Algorithm

	if sig.Sign(privkey.(*ecdsa.PrivateKey), []RR{a}) != nil {
		t.Fatal("failure to sign the record")
	}

	if err := sig.Verify(eckey.(*DNSKEY), []RR{a}); err != nil {
		t.Fatalf("failure to validate:\n%s\n%s\n%s\n\n%s\n\n%v",
			eckey.(*DNSKEY).String(),
			a.String(),
			sig.String(),
			eckey.(*DNSKEY).PrivateKeyString(privkey),
			err,
		)
	}
}

func TestSignVerifyECDSA2(t *testing.T) {
	srv1 := testRR("srv.miek.nl. IN SRV 1000 800 0 web1.miek.nl.")
	srv := srv1.(*SRV)

	// With this key
	key := new(DNSKEY)
	key.Hdr.Rrtype = TypeDNSKEY
	key.Hdr.Name = "miek.nl."
	key.Hdr.Class = ClassINET
	key.Hdr.Ttl = 14400
	key.Flags = 256
	key.Protocol = 3
	key.Algorithm = ECDSAP256SHA256
	privkey, err := key.Generate(256)
	if err != nil {
		t.Fatal("failure to generate key")
	}

	// Fill in the values of the Sig, before signing
	sig := new(RRSIG)
	sig.Hdr = RR_Header{"miek.nl.", TypeRRSIG, ClassINET, 14400, 0}
	sig.TypeCovered = srv.Hdr.Rrtype
	sig.Labels = uint8(CountLabel(srv.Hdr.Name)) // works for all 3
	sig.OrigTtl = srv.Hdr.Ttl
	sig.Expiration = 1296534305 // date -u '+%s' -d"2011-02-01 04:25:05"
	sig.Inception = 1293942305  // date -u '+%s' -d"2011-01-02 04:25:05"
	sig.KeyTag = key.KeyTag()   // Get the keyfrom the Key
	sig.SignerName = key.Hdr.Name
	sig.Algorithm = ECDSAP256SHA256

	if sig.Sign(privkey.(*ecdsa.PrivateKey), []RR{srv}) != nil {
		t.Fatal("failure to sign the record")
	}

	err = sig.Verify(key, []RR{srv})
	if err != nil {
		t.Errorf("failure to validate:\n%s\n%s\n%s\n\n%s\n\n%v",
			key.String(),
			srv.String(),
			sig.String(),
			key.PrivateKeyString(privkey),
			err,
		)
	}
}

func TestSignVerifyEd25519(t *testing.T) {
	srv1, err := NewRR("srv.miek.nl. IN SRV 1000 800 0 web1.miek.nl.")
	if err != nil {
		t.Fatal(err)
	}
	srv := srv1.(*SRV)

	// With this key
	key := new(DNSKEY)
	key.Hdr.Rrtype = TypeDNSKEY
	key.Hdr.Name = "miek.nl."
	key.Hdr.Class = ClassINET
	key.Hdr.Ttl = 14400
	key.Flags = 256
	key.Protocol = 3
	key.Algorithm = ED25519
	privkey, err := key.Generate(256)
	if err != nil {
		t.Fatal("failure to generate key")
	}

	// Fill in the values of the Sig, before signing
	sig := new(RRSIG)
	sig.Hdr = RR_Header{"miek.nl.", TypeRRSIG, ClassINET, 14400, 0}
	sig.TypeCovered = srv.Hdr.Rrtype
	sig.Labels = uint8(CountLabel(srv.Hdr.Name)) // works for all 3
	sig.OrigTtl = srv.Hdr.Ttl
	sig.Expiration = 1296534305 // date -u '+%s' -d"2011-02-01 04:25:05"
	sig.Inception = 1293942305  // date -u '+%s' -d"2011-01-02 04:25:05"
	sig.KeyTag = key.KeyTag()   // Get the keyfrom the Key
	sig.SignerName = key.Hdr.Name
	sig.Algorithm = ED25519

	if sig.Sign(privkey.(ed25519.PrivateKey), []RR{srv}) != nil {
		t.Fatal("failure to sign the record")
	}

	err = sig.Verify(key, []RR{srv})
	if err != nil {
		t.Logf("failure to validate:\n%s\n%s\n%s\n\n%s\n\n%v",
			key.String(),
			srv.String(),
			sig.String(),
			key.PrivateKeyString(privkey),
			err,
		)
	}
}

// Here the test vectors from the relevant RFCs are checked.
// rfc6605 6.1
func TestRFC6605P256(t *testing.T) {
	exDNSKEY := `example.net. 3600 IN DNSKEY 257 3 13 (
                 GojIhhXUN/u4v54ZQqGSnyhWJwaubCvTmeexv7bR6edb
                 krSqQpF64cYbcB7wNcP+e+MAnLr+Wi9xMWyQLc8NAA== )`
	exPriv := `Private-key-format: v1.2
Algorithm: 13 (ECDSAP256SHA256)
PrivateKey: GU6SnQ/Ou+xC5RumuIUIuJZteXT2z0O/ok1s38Et6mQ=`
	rrDNSKEY := testRR(exDNSKEY)
	priv, err := rrDNSKEY.(*DNSKEY).NewPrivateKey(exPriv)
	if err != nil {
		t.Fatal(err)
	}

	exDS := `example.net. 3600 IN DS 55648 13 2 (
             b4c8c1fe2e7477127b27115656ad6256f424625bf5c1
             e2770ce6d6e37df61d17 )`
	rrDS := testRR(exDS)
	ourDS := rrDNSKEY.(*DNSKEY).ToDS(SHA256)
	if !reflect.DeepEqual(ourDS, rrDS.(*DS)) {
		t.Errorf("DS record differs:\n%v\n%v", ourDS, rrDS.(*DS))
	}

	exA := `www.example.net. 3600 IN A 192.0.2.1`
	exRRSIG := `www.example.net. 3600 IN RRSIG A 13 3 3600 (
                20100909100439 20100812100439 55648 example.net.
                qx6wLYqmh+l9oCKTN6qIc+bw6ya+KJ8oMz0YP107epXA
                yGmt+3SNruPFKG7tZoLBLlUzGGus7ZwmwWep666VCw== )`
	rrA := testRR(exA)
	rrRRSIG := testRR(exRRSIG)
	if err := rrRRSIG.(*RRSIG).Verify(rrDNSKEY.(*DNSKEY), []RR{rrA}); err != nil {
		t.Errorf("failure to validate the spec RRSIG: %v", err)
	}

	ourRRSIG := &RRSIG{
		Hdr: RR_Header{
			Ttl: rrA.Header().Ttl,
		},
		KeyTag:     rrDNSKEY.(*DNSKEY).KeyTag(),
		SignerName: rrDNSKEY.(*DNSKEY).Hdr.Name,
		Algorithm:  rrDNSKEY.(*DNSKEY).Algorithm,
	}
	ourRRSIG.Expiration, _ = StringToTime("20100909100439")
	ourRRSIG.Inception, _ = StringToTime("20100812100439")
	err = ourRRSIG.Sign(priv.(*ecdsa.PrivateKey), []RR{rrA})
	if err != nil {
		t.Fatal(err)
	}

	if err = ourRRSIG.Verify(rrDNSKEY.(*DNSKEY), []RR{rrA}); err != nil {
		t.Errorf("failure to validate our RRSIG: %v", err)
	}

	// Signatures are randomized
	rrRRSIG.(*RRSIG).Signature = ""
	ourRRSIG.Signature = ""
	if !reflect.DeepEqual(ourRRSIG, rrRRSIG.(*RRSIG)) {
		t.Fatalf("RRSIG record differs:\n%v\n%v", ourRRSIG, rrRRSIG.(*RRSIG))
	}
}

// rfc6605 6.2
func TestRFC6605P384(t *testing.T) {
	exDNSKEY := `example.net. 3600 IN DNSKEY 257 3 14 (
                 xKYaNhWdGOfJ+nPrL8/arkwf2EY3MDJ+SErKivBVSum1
                 w/egsXvSADtNJhyem5RCOpgQ6K8X1DRSEkrbYQ+OB+v8
                 /uX45NBwY8rp65F6Glur8I/mlVNgF6W/qTI37m40 )`
	exPriv := `Private-key-format: v1.2
Algorithm: 14 (ECDSAP384SHA384)
PrivateKey: WURgWHCcYIYUPWgeLmiPY2DJJk02vgrmTfitxgqcL4vwW7BOrbawVmVe0d9V94SR`
	rrDNSKEY := testRR(exDNSKEY)
	priv, err := rrDNSKEY.(*DNSKEY).NewPrivateKey(exPriv)
	if err != nil {
		t.Fatal(err)
	}

	exDS := `example.net. 3600 IN DS 10771 14 4 (
           72d7b62976ce06438e9c0bf319013cf801f09ecc84b8
           d7e9495f27e305c6a9b0563a9b5f4d288405c3008a94
           6df983d6 )`
	rrDS := testRR(exDS)
	ourDS := rrDNSKEY.(*DNSKEY).ToDS(SHA384)
	if !reflect.DeepEqual(ourDS, rrDS.(*DS)) {
		t.Fatalf("DS record differs:\n%v\n%v", ourDS, rrDS.(*DS))
	}

	exA := `www.example.net. 3600 IN A 192.0.2.1`
	exRRSIG := `www.example.net. 3600 IN RRSIG A 14 3 3600 (
           20100909102025 20100812102025 10771 example.net.
           /L5hDKIvGDyI1fcARX3z65qrmPsVz73QD1Mr5CEqOiLP
           95hxQouuroGCeZOvzFaxsT8Glr74hbavRKayJNuydCuz
           WTSSPdz7wnqXL5bdcJzusdnI0RSMROxxwGipWcJm )`
	rrA := testRR(exA)
	rrRRSIG := testRR(exRRSIG)
	if err != nil {
		t.Fatal(err)
	}
	if err = rrRRSIG.(*RRSIG).Verify(rrDNSKEY.(*DNSKEY), []RR{rrA}); err != nil {
		t.Errorf("failure to validate the spec RRSIG: %v", err)
	}

	ourRRSIG := &RRSIG{
		Hdr: RR_Header{
			Ttl: rrA.Header().Ttl,
		},
		KeyTag:     rrDNSKEY.(*DNSKEY).KeyTag(),
		SignerName: rrDNSKEY.(*DNSKEY).Hdr.Name,
		Algorithm:  rrDNSKEY.(*DNSKEY).Algorithm,
	}
	ourRRSIG.Expiration, _ = StringToTime("20100909102025")
	ourRRSIG.Inception, _ = StringToTime("20100812102025")
	err = ourRRSIG.Sign(priv.(*ecdsa.PrivateKey), []RR{rrA})
	if err != nil {
		t.Fatal(err)
	}

	if err = ourRRSIG.Verify(rrDNSKEY.(*DNSKEY), []RR{rrA}); err != nil {
		t.Errorf("failure to validate our RRSIG: %v", err)
	}

	// Signatures are randomized
	rrRRSIG.(*RRSIG).Signature = ""
	ourRRSIG.Signature = ""
	if !reflect.DeepEqual(ourRRSIG, rrRRSIG.(*RRSIG)) {
		t.Fatalf("RRSIG record differs:\n%v\n%v", ourRRSIG, rrRRSIG.(*RRSIG))
	}
}

// rfc8080 6.1
func TestRFC8080Ed25519Example1(t *testing.T) {
	exDNSKEY := `example.com. 3600 IN DNSKEY 257 3 15 (
             l02Woi0iS8Aa25FQkUd9RMzZHJpBoRQwAQEX1SxZJA4= )`
	exPriv := `Private-key-format: v1.2
Algorithm: 15 (ED25519)
PrivateKey: ODIyNjAzODQ2MjgwODAxMjI2NDUxOTAyMDQxNDIyNjI=`
	rrDNSKEY, err := NewRR(exDNSKEY)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := rrDNSKEY.(*DNSKEY).NewPrivateKey(exPriv)
	if err != nil {
		t.Fatal(err)
	}

	exDS := `example.com. 3600 IN DS 3613 15 2 (
             3aa5ab37efce57f737fc1627013fee07bdf241bd10f3b1964ab55c78e79
             a304b )`
	rrDS, err := NewRR(exDS)
	if err != nil {
		t.Fatal(err)
	}
	ourDS := rrDNSKEY.(*DNSKEY).ToDS(SHA256)
	if !reflect.DeepEqual(ourDS, rrDS.(*DS)) {
		t.Fatalf("DS record differs:\n%v\n%v", ourDS, rrDS.(*DS))
	}

	exMX := `example.com. 3600 IN MX 10 mail.example.com.`
	exRRSIG := `example.com. 3600 IN RRSIG MX 15 2 3600 (
             1440021600 1438207200 3613 example.com. (
             oL9krJun7xfBOIWcGHi7mag5/hdZrKWw15jPGrHpjQeRAvTdszaPD+QLs3f
             x8A4M3e23mRZ9VrbpMngwcrqNAg== ) )`
	rrMX, err := NewRR(exMX)
	if err != nil {
		t.Fatal(err)
	}
	rrRRSIG, err := NewRR(exRRSIG)
	if err != nil {
		t.Fatal(err)
	}
	if err = rrRRSIG.(*RRSIG).Verify(rrDNSKEY.(*DNSKEY), []RR{rrMX}); err != nil {
		t.Errorf("failure to validate the spec RRSIG: %v", err)
	}

	ourRRSIG := &RRSIG{
		Hdr: RR_Header{
			Ttl: rrMX.Header().Ttl,
		},
		KeyTag:     rrDNSKEY.(*DNSKEY).KeyTag(),
		SignerName: rrDNSKEY.(*DNSKEY).Hdr.Name,
		Algorithm:  rrDNSKEY.(*DNSKEY).Algorithm,
	}
	ourRRSIG.Expiration, _ = StringToTime("20150819220000")
	ourRRSIG.Inception, _ = StringToTime("20150729220000")
	err = ourRRSIG.Sign(priv.(ed25519.PrivateKey), []RR{rrMX})
	if err != nil {
		t.Fatal(err)
	}

	if err = ourRRSIG.Verify(rrDNSKEY.(*DNSKEY), []RR{rrMX}); err != nil {
		t.Errorf("failure to validate our RRSIG: %v", err)
	}

	if !reflect.DeepEqual(ourRRSIG, rrRRSIG.(*RRSIG)) {
		t.Fatalf("RRSIG record differs:\n%v\n%v", ourRRSIG, rrRRSIG.(*RRSIG))
	}
}

// rfc8080 6.1
func TestRFC8080Ed25519Example2(t *testing.T) {
	exDNSKEY := `example.com. 3600 IN DNSKEY 257 3 15 (
             zPnZ/QwEe7S8C5SPz2OfS5RR40ATk2/rYnE9xHIEijs= )`
	exPriv := `Private-key-format: v1.2
Algorithm: 15 (ED25519)
PrivateKey: DSSF3o0s0f+ElWzj9E/Osxw8hLpk55chkmx0LYN5WiY=`
	rrDNSKEY, err := NewRR(exDNSKEY)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := rrDNSKEY.(*DNSKEY).NewPrivateKey(exPriv)
	if err != nil {
		t.Fatal(err)
	}

	exDS := `example.com. 3600 IN DS 35217 15 2 (
             401781b934e392de492ec77ae2e15d70f6575a1c0bc59c5275c04ebe80c
             6614c )`
	rrDS, err := NewRR(exDS)
	if err != nil {
		t.Fatal(err)
	}
	ourDS := rrDNSKEY.(*DNSKEY).ToDS(SHA256)
	if !reflect.DeepEqual(ourDS, rrDS.(*DS)) {
		t.Fatalf("DS record differs:\n%v\n%v", ourDS, rrDS.(*DS))
	}

	exMX := `example.com. 3600 IN MX 10 mail.example.com.`
	exRRSIG := `example.com. 3600 IN RRSIG MX 15 2 3600 (
             1440021600 1438207200 35217 example.com. (
             zXQ0bkYgQTEFyfLyi9QoiY6D8ZdYo4wyUhVioYZXFdT410QPRITQSqJSnzQ
             oSm5poJ7gD7AQR0O7KuI5k2pcBg== ) )`
	rrMX, err := NewRR(exMX)
	if err != nil {
		t.Fatal(err)
	}
	rrRRSIG, err := NewRR(exRRSIG)
	if err != nil {
		t.Fatal(err)
	}
	if err = rrRRSIG.(*RRSIG).Verify(rrDNSKEY.(*DNSKEY), []RR{rrMX}); err != nil {
		t.Errorf("failure to validate the spec RRSIG: %v", err)
	}

	ourRRSIG := &RRSIG{
		Hdr: RR_Header{
			Ttl: rrMX.Header().Ttl,
		},
		KeyTag:     rrDNSKEY.(*DNSKEY).KeyTag(),
		SignerName: rrDNSKEY.(*DNSKEY).Hdr.Name,
		Algorithm:  rrDNSKEY.(*DNSKEY).Algorithm,
	}
	ourRRSIG.Expiration, _ = StringToTime("20150819220000")
	ourRRSIG.Inception, _ = StringToTime("20150729220000")
	err = ourRRSIG.Sign(priv.(ed25519.PrivateKey), []RR{rrMX})
	if err != nil {
		t.Fatal(err)
	}

	if err = ourRRSIG.Verify(rrDNSKEY.(*DNSKEY), []RR{rrMX}); err != nil {
		t.Errorf("failure to validate our RRSIG: %v", err)
	}

	if !reflect.DeepEqual(ourRRSIG, rrRRSIG.(*RRSIG)) {
		t.Fatalf("RRSIG record differs:\n%v\n%v", ourRRSIG, rrRRSIG.(*RRSIG))
	}
}

func TestInvalidRRSet(t *testing.T) {
	goodRecords := make([]RR, 2)
	goodRecords[0] = &TXT{Hdr: RR_Header{Name: "name.cloudflare.com.", Rrtype: TypeTXT, Class: ClassINET, Ttl: 0}, Txt: []string{"Hello world"}}
	goodRecords[1] = &TXT{Hdr: RR_Header{Name: "name.cloudflare.com.", Rrtype: TypeTXT, Class: ClassINET, Ttl: 0}, Txt: []string{"_o/"}}

	// Generate key
	keyname := "cloudflare.com."
	key := &DNSKEY{
		Hdr:       RR_Header{Name: keyname, Rrtype: TypeDNSKEY, Class: ClassINET, Ttl: 0},
		Algorithm: ECDSAP256SHA256,
		Flags:     ZONE,
		Protocol:  3,
	}
	privatekey, err := key.Generate(256)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Need to fill in: Inception, Expiration, KeyTag, SignerName and Algorithm
	curTime := time.Now()
	signature := &RRSIG{
		Inception:  uint32(curTime.Unix()),
		Expiration: uint32(curTime.Add(time.Hour).Unix()),
		KeyTag:     key.KeyTag(),
		SignerName: keyname,
		Algorithm:  ECDSAP256SHA256,
	}

	// Inconsistent name between records
	badRecords := make([]RR, 2)
	badRecords[0] = &TXT{Hdr: RR_Header{Name: "name.cloudflare.com.", Rrtype: TypeTXT, Class: ClassINET, Ttl: 0}, Txt: []string{"Hello world"}}
	badRecords[1] = &TXT{Hdr: RR_Header{Name: "nama.cloudflare.com.", Rrtype: TypeTXT, Class: ClassINET, Ttl: 0}, Txt: []string{"_o/"}}

	if IsRRset(badRecords) {
		t.Fatal("Record set with inconsistent names considered valid")
	}

	badRecords[0] = &TXT{Hdr: RR_Header{Name: "name.cloudflare.com.", Rrtype: TypeTXT, Class: ClassINET, Ttl: 0}, Txt: []string{"Hello world"}}
	badRecords[1] = &A{Hdr: RR_Header{Name: "name.cloudflare.com.", Rrtype: TypeA, Class: ClassINET, Ttl: 0}}

	if IsRRset(badRecords) {
		t.Fatal("Record set with inconsistent record types considered valid")
	}

	badRecords[0] = &TXT{Hdr: RR_Header{Name: "name.cloudflare.com.", Rrtype: TypeTXT, Class: ClassINET, Ttl: 0}, Txt: []string{"Hello world"}}
	badRecords[1] = &TXT{Hdr: RR_Header{Name: "name.cloudflare.com.", Rrtype: TypeTXT, Class: ClassCHAOS, Ttl: 0}, Txt: []string{"_o/"}}

	if IsRRset(badRecords) {
		t.Fatal("Record set with inconsistent record class considered valid")
	}

	// Sign the good record set and then make sure verification fails on the bad record set
	if err := signature.Sign(privatekey.(crypto.Signer), goodRecords); err != nil {
		t.Fatal("Signing good records failed")
	}

	if err := signature.Verify(key, badRecords); err != ErrRRset {
		t.Fatal("Verification did not return ErrRRset with inconsistent records")
	}
}
