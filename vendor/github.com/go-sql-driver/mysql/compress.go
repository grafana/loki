// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2024 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"sync"
)

var (
	zrPool *sync.Pool // Do not use directly. Use zDecompress() instead.
	zwPool *sync.Pool // Do not use directly. Use zCompress() instead.
)

func init() {
	zrPool = &sync.Pool{
		New: func() any { return nil },
	}
	zwPool = &sync.Pool{
		New: func() any {
			zw, err := zlib.NewWriterLevel(new(bytes.Buffer), 2)
			if err != nil {
				panic(err) // compress/zlib return non-nil error only if level is invalid
			}
			return zw
		},
	}
}

func zDecompress(src []byte, dst *bytes.Buffer) (int, error) {
	br := bytes.NewReader(src)
	var zr io.ReadCloser
	var err error

	if a := zrPool.Get(); a == nil {
		if zr, err = zlib.NewReader(br); err != nil {
			return 0, err
		}
	} else {
		zr = a.(io.ReadCloser)
		if err := zr.(zlib.Resetter).Reset(br, nil); err != nil {
			return 0, err
		}
	}

	n, _ := dst.ReadFrom(zr) // ignore err because zr.Close() will return it again.
	err = zr.Close()         // zr.Close() may return chuecksum error.
	zrPool.Put(zr)
	return int(n), err
}

func zCompress(src []byte, dst io.Writer) error {
	zw := zwPool.Get().(*zlib.Writer)
	zw.Reset(dst)
	if _, err := zw.Write(src); err != nil {
		return err
	}
	err := zw.Close()
	zwPool.Put(zw)
	return err
}

type compIO struct {
	mc   *mysqlConn
	buff bytes.Buffer
}

func newCompIO(mc *mysqlConn) *compIO {
	return &compIO{
		mc: mc,
	}
}

func (c *compIO) reset() {
	c.buff.Reset()
}

func (c *compIO) readNext(need int) ([]byte, error) {
	for c.buff.Len() < need {
		if err := c.readCompressedPacket(); err != nil {
			return nil, err
		}
	}
	data := c.buff.Next(need)
	return data[:need:need], nil // prevent caller writes into c.buff
}

func (c *compIO) readCompressedPacket() error {
	header, err := c.mc.readNext(7)
	if err != nil {
		return err
	}
	_ = header[6] // bounds check hint to compiler; guaranteed by readNext

	// compressed header structure
	comprLength := getUint24(header[0:3])
	compressionSequence := header[3]
	uncompressedLength := getUint24(header[4:7])
	if debug {
		fmt.Printf("uncompress cmplen=%v uncomplen=%v pkt_cmp_seq=%v expected_cmp_seq=%v\n",
			comprLength, uncompressedLength, compressionSequence, c.mc.sequence)
	}
	// Do not return ErrPktSync here.
	// Server may return error packet (e.g. 1153 Got a packet bigger than 'max_allowed_packet' bytes)
	// before receiving all packets from client. In this case, seqnr is younger than expected.
	// NOTE: Both of mariadbclient and mysqlclient do not check seqnr. Only server checks it.
	if debug && compressionSequence != c.mc.compressSequence {
		fmt.Printf("WARN: unexpected cmpress seq nr: expected %v, got %v",
			c.mc.compressSequence, compressionSequence)
	}
	c.mc.compressSequence = compressionSequence + 1

	comprData, err := c.mc.readNext(comprLength)
	if err != nil {
		return err
	}

	// if payload is uncompressed, its length will be specified as zero, and its
	// true length is contained in comprLength
	if uncompressedLength == 0 {
		c.buff.Write(comprData)
		return nil
	}

	// use existing capacity in bytesBuf if possible
	c.buff.Grow(uncompressedLength)
	nread, err := zDecompress(comprData, &c.buff)
	if err != nil {
		return err
	}
	if nread != uncompressedLength {
		return fmt.Errorf("invalid compressed packet: uncompressed length in header is %d, actual %d",
			uncompressedLength, nread)
	}
	return nil
}

const minCompressLength = 150
const maxPayloadLen = maxPacketSize - 4

// writePackets sends one or some packets with compression.
// Use this instead of mc.netConn.Write() when mc.compress is true.
func (c *compIO) writePackets(packets []byte) (int, error) {
	totalBytes := len(packets)
	blankHeader := make([]byte, 7)
	buf := &c.buff

	for len(packets) > 0 {
		payloadLen := min(maxPayloadLen, len(packets))
		payload := packets[:payloadLen]
		uncompressedLen := payloadLen

		buf.Reset()
		buf.Write(blankHeader) // Buffer.Write() never returns error

		// If payload is less than minCompressLength, don't compress.
		if uncompressedLen < minCompressLength {
			buf.Write(payload)
			uncompressedLen = 0
		} else {
			err := zCompress(payload, buf)
			if debug && err != nil {
				fmt.Printf("zCompress error: %v", err)
			}
			// do not compress if compressed data is larger than uncompressed data
			// I intentionally miss 7 byte header in the buf; zCompress must compress more than 7 bytes.
			if err != nil || buf.Len() >= uncompressedLen {
				buf.Reset()
				buf.Write(blankHeader)
				buf.Write(payload)
				uncompressedLen = 0
			}
		}

		if n, err := c.writeCompressedPacket(buf.Bytes(), uncompressedLen); err != nil {
			// To allow returning ErrBadConn when sending really 0 bytes, we sum
			// up compressed bytes that is returned by underlying Write().
			return totalBytes - len(packets) + n, err
		}
		packets = packets[payloadLen:]
	}

	return totalBytes, nil
}

// writeCompressedPacket writes a compressed packet with header.
// data should start with 7 size space for header followed by payload.
func (c *compIO) writeCompressedPacket(data []byte, uncompressedLen int) (int, error) {
	mc := c.mc
	comprLength := len(data) - 7
	if debug {
		fmt.Printf(
			"writeCompressedPacket: comprLength=%v, uncompressedLen=%v, seq=%v\n",
			comprLength, uncompressedLen, mc.compressSequence)
	}

	// compression header
	putUint24(data[0:3], comprLength)
	data[3] = mc.compressSequence
	putUint24(data[4:7], uncompressedLen)

	mc.compressSequence++
	return mc.writeWithTimeout(data)
}
