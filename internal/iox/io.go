// Package iox provides exact-length TCP I/O helpers.
package iox

import (
	"io"
	"net"
	"time"
)

// ReadExact reads exactly n bytes from r, looping until complete or error.
func ReadExact(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// WriteExact writes all of buf to w, looping until complete or error.
func WriteExact(w io.Writer, buf []byte) error {
	for len(buf) > 0 {
		n, err := w.Write(buf)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		buf = buf[n:]
	}
	return nil
}

// ConnWithDeadline is a net.Conn that can have per-operation deadlines set.
type ConnWithDeadline interface {
	net.Conn
}

// SetRWDeadline sets both read and write deadlines relative to now.
func SetRWDeadline(c net.Conn, d time.Duration) error {
	if d <= 0 {
		return c.SetDeadline(time.Time{})
	}
	return c.SetDeadline(time.Now().Add(d))
}
