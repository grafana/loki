/*
* Licensed to the Apache Software Foundation (ASF) under one
* or more contributor license agreements. See the NOTICE file
* distributed with this work for additional information
* regarding copyright ownership. The ASF licenses this file
* to you under the Apache License, Version 2.0 (the
* "License"); you may not use this file except in compliance
* with the License. You may obtain a copy of the License at
*
*   http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing,
* software distributed under the License is distributed on an
* "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
* KIND, either express or implied. See the License for the
* specific language governing permissions and limitations
* under the License.
 */

package thrift

import (
	"compress/zlib"
	"context"
	"fmt"
	"io"
)

// TZlibTransportFactory is a factory for TZlibTransport instances
type TZlibTransportFactory struct {
	level   int
	factory TTransportFactory
	cfg     *TConfiguration
}

// TZlibTransport is a TTransport implementation that makes use of zlib compression.
type TZlibTransport struct {
	reader      io.ReadCloser
	transport   TTransport
	writer      *zlib.Writer
	writeCloser io.Closer
	conf        *TConfiguration
	bytesRead   int64
}

// GetTransport constructs a new instance of NewTZlibTransport
func (p *TZlibTransportFactory) GetTransport(trans TTransport) (TTransport, error) {
	if p.factory != nil {
		// wrap other factory
		PropagateTConfiguration(trans, p.cfg)
		var err error
		trans, err = p.factory.GetTransport(trans)
		if err != nil {
			return nil, err
		}
	}
	return NewTZlibTransportConf(trans, p.level, p.cfg)
}

// SetTConfiguration implements TConfigurationSetter for propagation.
func (p *TZlibTransportFactory) SetTConfiguration(conf *TConfiguration) {
	PropagateTConfiguration(p.factory, conf)
	p.cfg = conf
}

var _ TConfigurationSetter = (*TZlibTransportFactory)(nil)

// NewTZlibTransportFactory constructs a new instance of NewTZlibTransportFactory
func NewTZlibTransportFactory(level int) *TZlibTransportFactory {
	return &TZlibTransportFactory{level: level, factory: nil}
}

// NewTZlibTransportFactoryWithFactory constructs a new instance of TZlibTransportFactory
// as a wrapper over existing transport factory
func NewTZlibTransportFactoryWithFactory(level int, factory TTransportFactory) *TZlibTransportFactory {
	return &TZlibTransportFactory{level: level, factory: factory}
}

// NewTZlibTransportFactoryConf constructs a new instance of TZlibTransportFactory
// whose transports use conf, as a wrapper over factory if it is not nil
func NewTZlibTransportFactoryConf(level int, factory TTransportFactory, conf *TConfiguration) *TZlibTransportFactory {
	PropagateTConfiguration(factory, conf)
	return &TZlibTransportFactory{level: level, factory: factory, cfg: conf}
}

// NewTZlibTransportConf constructs a new instance of TZlibTransport that uses
// conf, and passes conf on to trans
func NewTZlibTransportConf(trans TTransport, level int, conf *TConfiguration) (*TZlibTransport, error) {
	z, err := NewTZlibTransport(trans, level)
	if err != nil {
		return nil, err
	}
	z.SetTConfiguration(conf)
	return z, nil
}

// NewTZlibTransport constructs a new instance of TZlibTransport
func NewTZlibTransport(trans TTransport, level int) (*TZlibTransport, error) {
	writer, closer, err := newZlibWriterCloserLevel(trans, level)
	if err != nil {
		return nil, err
	}
	return &TZlibTransport{
		writer:      writer,
		writeCloser: closer,
		transport:   trans,
	}, nil
}

// Close closes the reader and writer (flushing any unwritten data) and closes
// the underlying transport.
func (z *TZlibTransport) Close() error {
	z.bytesRead = 0
	if z.reader != nil {
		if err := z.reader.Close(); err != nil {
			return err
		}
	}
	if err := z.writeCloser.Close(); err != nil {
		return err
	}
	return z.transport.Close()
}

// Flush flushes the writer and its underlying transport.
func (z *TZlibTransport) Flush(ctx context.Context) error {
	if err := z.writer.Flush(); err != nil {
		return err
	}
	return z.transport.Flush(ctx)
}

// IsOpen returns true if the transport is open
func (z *TZlibTransport) IsOpen() bool {
	return z.transport.IsOpen()
}

// Open opens the transport for communication
func (z *TZlibTransport) Open() error {
	return z.transport.Open()
}

func (z *TZlibTransport) Read(p []byte) (int, error) {
	if z.reader == nil {
		r, err := newZlibReader(z.transport)
		if err != nil {
			return 0, NewTTransportExceptionFromError(err)
		}
		z.reader = r
	}

	n, err := z.reader.Read(p)
	if n > 0 {
		z.bytesRead += int64(n)
		if maxSize := int64(z.conf.GetMaxMessageSize()); z.bytesRead > maxSize {
			return n, NewTProtocolExceptionWithType(
				SIZE_LIMIT,
				fmt.Errorf("decompressed size exceeded limit of %d bytes", maxSize),
			)
		}
	}
	return n, err
}

// RemainingBytes returns the size in bytes of the data that is still to be
// read.
//
// The underlying transport counts compressed bytes, which are no bound on the
// bytes this transport can still produce, so the answer is always
// UnknownRemainingBytes.
func (z *TZlibTransport) RemainingBytes() uint64 {
	return UnknownRemainingBytes
}

func (z *TZlibTransport) Write(p []byte) (int, error) {
	return z.writer.Write(p)
}

// SetTConfiguration implements TConfigurationSetter for propagation.
func (z *TZlibTransport) SetTConfiguration(conf *TConfiguration) {
	z.conf = conf
	PropagateTConfiguration(z.transport, conf)
}

var _ TConfigurationSetter = (*TZlibTransport)(nil)
