package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var (
	ErrBadMagic         = errors.New("protocol: bad magic")
	ErrUnsupportedVer   = errors.New("protocol: unsupported version")
	ErrBadReserved      = errors.New("protocol: reserved field must be zero")
	ErrPayloadTooLarge  = errors.New("protocol: payload too large")
	ErrHeaderCount      = errors.New("protocol: header count exceeds limit")
	ErrTruncated        = errors.New("protocol: truncated input")
	ErrMalformed        = errors.New("protocol: malformed frame")
	ErrUnknownFrameType = errors.New("protocol: unknown frame type")
	ErrBadPathLength    = errors.New("protocol: invalid path length")
	ErrBadHeaderLength  = errors.New("protocol: invalid header length")
	ErrBadMethod        = errors.New("protocol: unsupported method")
	ErrBadHeaderID      = errors.New("protocol: unknown header id")
)

// FrameHeader is the fixed 16-byte BP/1 frame header.
type FrameHeader struct {
	Magic0        byte
	Magic1        byte
	Version       byte
	Type          byte
	Flags         byte
	Reserved      byte
	HeaderCount   uint16
	PayloadLength uint32
	RequestID     uint32
}

// Header is one dictionary-encoded header entry.
type Header struct {
	ID    byte
	Value []byte
}

// Request is a decoded REQUEST payload.
type Request struct {
	Method  byte
	Path    string
	Headers []Header
}

// Response is a decoded RESPONSE payload.
type Response struct {
	Status  uint16
	Headers []Header
	Body    []byte
}

// EncodeFrameHeader writes the 16-byte header into buf (must be >= 16 bytes).
func EncodeFrameHeader(h FrameHeader, buf []byte) {
	buf[0] = h.Magic0
	buf[1] = h.Magic1
	buf[2] = h.Version
	buf[3] = h.Type
	buf[4] = h.Flags
	buf[5] = h.Reserved
	binary.BigEndian.PutUint16(buf[6:8], h.HeaderCount)
	binary.BigEndian.PutUint32(buf[8:12], h.PayloadLength)
	binary.BigEndian.PutUint32(buf[12:16], h.RequestID)
}

// DecodeFrameHeader parses and validates a 16-byte header.
// Unknown frame types are accepted at this layer (caller skips payload).
func DecodeFrameHeader(buf []byte) (FrameHeader, error) {
	if len(buf) < FrameHeaderSize {
		return FrameHeader{}, ErrTruncated
	}
	h := FrameHeader{
		Magic0:        buf[0],
		Magic1:        buf[1],
		Version:       buf[2],
		Type:          buf[3],
		Flags:         buf[4],
		Reserved:      buf[5],
		HeaderCount:   binary.BigEndian.Uint16(buf[6:8]),
		PayloadLength: binary.BigEndian.Uint32(buf[8:12]),
		RequestID:     binary.BigEndian.Uint32(buf[12:16]),
	}
	if h.Magic0 != Magic0 || h.Magic1 != Magic1 {
		return FrameHeader{}, ErrBadMagic
	}
	if h.Version != Version1 {
		return FrameHeader{}, ErrUnsupportedVer
	}
	if h.Reserved != 0 {
		return FrameHeader{}, ErrBadReserved
	}
	if h.PayloadLength > MaxFramePayload {
		return FrameHeader{}, ErrPayloadTooLarge
	}
	if h.HeaderCount > MaxHeaderCount {
		return FrameHeader{}, ErrHeaderCount
	}
	return h, nil
}

// IsKnownFrameType reports whether t is REQUEST or RESPONSE.
func IsKnownFrameType(t byte) bool {
	return t == FrameTypeRequest || t == FrameTypeResponse
}

// EncodeHeaders serializes headers into a byte slice.
func EncodeHeaders(headers []Header) ([]byte, error) {
	if len(headers) > MaxHeaderCount {
		return nil, ErrHeaderCount
	}
	var out []byte
	for _, h := range headers {
		if _, ok := HeaderNames[h.ID]; !ok {
			return nil, ErrBadHeaderID
		}
		if len(h.Value) > MaxHeaderValueLength {
			return nil, ErrBadHeaderLength
		}
		var entry [3]byte
		entry[0] = h.ID
		binary.BigEndian.PutUint16(entry[1:3], uint16(len(h.Value)))
		out = append(out, entry[:]...)
		out = append(out, h.Value...)
	}
	return out, nil
}

// DecodeHeaders parses exactly count headers from data, returning remaining bytes.
func DecodeHeaders(data []byte, count uint16) ([]Header, []byte, error) {
	headers := make([]Header, 0, count)
	rest := data
	for i := 0; i < int(count); i++ {
		if len(rest) < 3 {
			return nil, nil, ErrTruncated
		}
		id := rest[0]
		vlen := binary.BigEndian.Uint16(rest[1:3])
		rest = rest[3:]
		if vlen > MaxHeaderValueLength {
			return nil, nil, ErrBadHeaderLength
		}
		if len(rest) < int(vlen) {
			return nil, nil, ErrTruncated
		}
		if _, ok := HeaderNames[id]; !ok {
			return nil, nil, ErrBadHeaderID
		}
		val := make([]byte, vlen)
		copy(val, rest[:vlen])
		rest = rest[vlen:]
		headers = append(headers, Header{ID: id, Value: val})
	}
	return headers, rest, nil
}

// EncodeRequestPayload builds the REQUEST payload bytes.
func EncodeRequestPayload(req Request) ([]byte, error) {
	if req.Method != MethodGET {
		return nil, ErrBadMethod
	}
	path := []byte(req.Path)
	if len(path) > MaxPathLength {
		return nil, ErrBadPathLength
	}
	hdrs, err := EncodeHeaders(req.Headers)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+2+len(path)+len(hdrs))
	out = append(out, req.Method)
	var plen [2]byte
	binary.BigEndian.PutUint16(plen[:], uint16(len(path)))
	out = append(out, plen[:]...)
	out = append(out, path...)
	out = append(out, hdrs...)
	return out, nil
}

// DecodeRequestPayload parses a REQUEST payload.
func DecodeRequestPayload(payload []byte, headerCount uint16) (Request, error) {
	if len(payload) < 3 {
		return Request{}, ErrTruncated
	}
	method := payload[0]
	if method != MethodGET {
		return Request{}, ErrBadMethod
	}
	pathLen := binary.BigEndian.Uint16(payload[1:3])
	if pathLen > MaxPathLength {
		return Request{}, ErrBadPathLength
	}
	rest := payload[3:]
	if len(rest) < int(pathLen) {
		return Request{}, ErrTruncated
	}
	path := string(rest[:pathLen])
	rest = rest[pathLen:]
	headers, rest, err := DecodeHeaders(rest, headerCount)
	if err != nil {
		return Request{}, err
	}
	if len(rest) != 0 {
		return Request{}, fmt.Errorf("%w: trailing bytes in request payload", ErrMalformed)
	}
	return Request{Method: method, Path: path, Headers: headers}, nil
}

// EncodeResponsePayload builds the RESPONSE payload bytes.
func EncodeResponsePayload(resp Response) ([]byte, error) {
	hdrs, err := EncodeHeaders(resp.Headers)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 2+len(hdrs)+len(resp.Body))
	var st [2]byte
	binary.BigEndian.PutUint16(st[:], resp.Status)
	out = append(out, st[:]...)
	out = append(out, hdrs...)
	out = append(out, resp.Body...)
	return out, nil
}

// DecodeResponsePayload parses a RESPONSE payload.
func DecodeResponsePayload(payload []byte, headerCount uint16) (Response, error) {
	if len(payload) < 2 {
		return Response{}, ErrTruncated
	}
	status := binary.BigEndian.Uint16(payload[0:2])
	headers, body, err := DecodeHeaders(payload[2:], headerCount)
	if err != nil {
		return Response{}, err
	}
	// Body is remaining payload after headers (may be empty).
	b := make([]byte, len(body))
	copy(b, body)
	return Response{Status: status, Headers: headers, Body: b}, nil
}

// BuildRequestFrame encodes a full REQUEST frame (header + payload).
func BuildRequestFrame(reqID uint32, req Request, flags byte) ([]byte, error) {
	payload, err := EncodeRequestPayload(req)
	if err != nil {
		return nil, err
	}
	if len(payload) > MaxFramePayload {
		return nil, ErrPayloadTooLarge
	}
	frame := make([]byte, FrameHeaderSize+len(payload))
	EncodeFrameHeader(FrameHeader{
		Magic0:        Magic0,
		Magic1:        Magic1,
		Version:       Version1,
		Type:          FrameTypeRequest,
		Flags:         flags,
		Reserved:      0,
		HeaderCount:   uint16(len(req.Headers)),
		PayloadLength: uint32(len(payload)),
		RequestID:     reqID,
	}, frame[:FrameHeaderSize])
	copy(frame[FrameHeaderSize:], payload)
	return frame, nil
}

// BuildResponseFrame encodes a full RESPONSE frame (header + payload).
func BuildResponseFrame(reqID uint32, resp Response, flags byte) ([]byte, error) {
	payload, err := EncodeResponsePayload(resp)
	if err != nil {
		return nil, err
	}
	if len(payload) > MaxFramePayload {
		return nil, ErrPayloadTooLarge
	}
	frame := make([]byte, FrameHeaderSize+len(payload))
	EncodeFrameHeader(FrameHeader{
		Magic0:        Magic0,
		Magic1:        Magic1,
		Version:       Version1,
		Type:          FrameTypeResponse,
		Flags:         flags,
		Reserved:      0,
		HeaderCount:   uint16(len(resp.Headers)),
		PayloadLength: uint32(len(payload)),
		RequestID:     reqID,
	}, frame[:FrameHeaderSize])
	copy(frame[FrameHeaderSize:], payload)
	return frame, nil
}

// ReadExact reads exactly n bytes from r.
func ReadExact(r io.Reader, n int) ([]byte, error) {
	if n < 0 {
		return nil, ErrMalformed
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrTruncated
		}
		return nil, err
	}
	return buf, nil
}

// DiscardExact discards exactly n bytes from r.
func DiscardExact(r io.Reader, n int) error {
	if n == 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, r, int64(n))
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrTruncated
		}
		return err
	}
	return nil
}

// ReadFrame reads one frame header and its payload.
// For unknown frame types, payload is still fully read (caller may discard it).
func ReadFrame(r io.Reader) (FrameHeader, []byte, error) {
	hdrBuf, err := ReadExact(r, FrameHeaderSize)
	if err != nil {
		return FrameHeader{}, nil, err
	}
	h, err := DecodeFrameHeader(hdrBuf)
	if err != nil {
		return FrameHeader{}, nil, err
	}
	payload, err := ReadExact(r, int(h.PayloadLength))
	if err != nil {
		return FrameHeader{}, nil, err
	}
	return h, payload, nil
}

// WriteAll writes all of buf to w.
func WriteAll(w io.Writer, buf []byte) error {
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

// HeaderString returns the string value of the first header with the given ID.
func HeaderString(headers []Header, id byte) (string, bool) {
	for _, h := range headers {
		if h.ID == id {
			return string(h.Value), true
		}
	}
	return "", false
}

// MakeHeader builds a Header from a string value.
func MakeHeader(id byte, value string) Header {
	return Header{ID: id, Value: []byte(value)}
}
