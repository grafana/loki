package testconn

import (
	"io"
	"net"
)

type Recorder struct {
	net.Conn
	w io.WriteCloser
}

func NewRecorder(w io.WriteCloser, conn net.Conn) Recorder {
	return Recorder{
		Conn: conn,
		w:    w,
	}
}

func (r Recorder) Read(b []byte) (int, error) {
	n, err := r.Conn.Read(b)
	r.w.Write(b[:n])
	r.w.Write([]byte("SPLIT\n"))
	return n, err
}

func (r Recorder) Close() error {
	err := r.Conn.Close()
	r.w.Close()
	return err
}
