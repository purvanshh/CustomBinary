package protocol_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"bp/internal/protocol"
)

func TestEncodeDecodeRequest(t *testing.T) {
	req := protocol.Request{
		Method: protocol.MethodGET,
		Path:   "/index.html",
		Headers: []protocol.Header{
			protocol.MakeHeader(protocol.HdrHost, "localhost"),
			protocol.MakeHeader(protocol.HdrUserAgent, "test"),
		},
	}
	frame, err := protocol.BuildRequestFrame(42, req, 0)
	if err != nil {
		t.Fatal(err)
	}
	h, err := protocol.DecodeFrameHeader(frame[:16])
	if err != nil {
		t.Fatal(err)
	}
	if h.Type != protocol.FrameTypeRequest || h.RequestID != 42 {
		t.Fatalf("header mismatch: %+v", h)
	}
	got, err := protocol.DecodeRequestPayload(frame[16:], h.HeaderCount)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/index.html" || got.Method != protocol.MethodGET {
		t.Fatalf("got %+v", got)
	}
	if len(got.Headers) != 2 {
		t.Fatalf("headers=%d", len(got.Headers))
	}
}

func TestEncodeDecodeResponseBinaryBody(t *testing.T) {
	body := []byte{0x00, 0xff, 0x10, 0x20, 0x42}
	resp := protocol.Response{
		Status: protocol.StatusOK,
		Headers: []protocol.Header{
			protocol.MakeHeader(protocol.HdrContentLength, "5"),
			protocol.MakeHeader(protocol.HdrContentType, "application/octet-stream"),
			protocol.MakeHeader(protocol.HdrConnection, "keep-alive"),
		},
		Body: body,
	}
	frame, err := protocol.BuildResponseFrame(7, resp, 0)
	if err != nil {
		t.Fatal(err)
	}
	h, err := protocol.DecodeFrameHeader(frame[:16])
	if err != nil {
		t.Fatal(err)
	}
	got, err := protocol.DecodeResponsePayload(frame[16:], h.HeaderCount)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != 200 || !bytes.Equal(got.Body, body) {
		t.Fatalf("got status=%d body=%v", got.Status, got.Body)
	}
}

func TestEmptyPayloadResponse(t *testing.T) {
	resp := protocol.Response{
		Status:  protocol.StatusOK,
		Headers: nil,
		Body:    nil,
	}
	payload, err := protocol.EncodeResponsePayload(resp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := protocol.DecodeResponsePayload(payload, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != 200 || len(got.Body) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestBadMagic(t *testing.T) {
	buf := make([]byte, 16)
	copy(buf, []byte{0x00, 0x00, 0x01, 0x01})
	_, err := protocol.DecodeFrameHeader(buf)
	if err != protocol.ErrBadMagic {
		t.Fatalf("want ErrBadMagic got %v", err)
	}
}

func TestUnsupportedVersion(t *testing.T) {
	buf := make([]byte, 16)
	buf[0], buf[1], buf[2], buf[3] = 0x42, 0x50, 0x99, 0x01
	_, err := protocol.DecodeFrameHeader(buf)
	if err != protocol.ErrUnsupportedVer {
		t.Fatalf("want ErrUnsupportedVer got %v", err)
	}
}

func TestTruncatedHeader(t *testing.T) {
	_, err := protocol.DecodeFrameHeader([]byte{0x42, 0x50})
	if err != protocol.ErrTruncated {
		t.Fatalf("want ErrTruncated got %v", err)
	}
}

func TestTruncatedPayload(t *testing.T) {
	req := protocol.Request{Method: protocol.MethodGET, Path: "/x"}
	frame, err := protocol.BuildRequestFrame(1, req, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Feed only header + partial payload.
	r := bytes.NewReader(frame[:len(frame)-1])
	_, _, err = protocol.ReadFrame(r)
	if err != protocol.ErrTruncated {
		t.Fatalf("want ErrTruncated got %v", err)
	}
}

func TestInvalidPathLength(t *testing.T) {
	// method + pathLen claiming more than available
	payload := []byte{0x01, 0x00, 0x10} // path len 16 but no path bytes
	_, err := protocol.DecodeRequestPayload(payload, 0)
	if err != protocol.ErrTruncated {
		t.Fatalf("want ErrTruncated got %v", err)
	}
}

func TestInvalidHeaderLength(t *testing.T) {
	// status + one header with value longer than remaining
	payload := []byte{
		0x00, 0xc8, // 200
		protocol.HdrContentType,
		0x00, 0x10, // value len 16
		'a', 'b', // only 2 bytes
	}
	_, err := protocol.DecodeResponsePayload(payload, 1)
	if err != protocol.ErrTruncated {
		t.Fatalf("want ErrTruncated got %v", err)
	}
}

func TestImpossiblePayloadSize(t *testing.T) {
	buf := make([]byte, 16)
	buf[0], buf[1], buf[2], buf[3] = 0x42, 0x50, 0x01, 0x01
	binary.BigEndian.PutUint32(buf[8:12], protocol.MaxFramePayload+1)
	_, err := protocol.DecodeFrameHeader(buf)
	if err != protocol.ErrPayloadTooLarge {
		t.Fatalf("want ErrPayloadTooLarge got %v", err)
	}
}

func TestPathLengthOverLimit(t *testing.T) {
	payload := make([]byte, 3+protocol.MaxPathLength+1)
	payload[0] = protocol.MethodGET
	binary.BigEndian.PutUint16(payload[1:3], uint16(protocol.MaxPathLength+1))
	_, err := protocol.DecodeRequestPayload(payload, 0)
	if err != protocol.ErrBadPathLength {
		t.Fatalf("want ErrBadPathLength got %v", err)
	}
}

func TestUnknownFrameSkip(t *testing.T) {
	// known request + unknown + known request streamed
	req1, _ := protocol.BuildRequestFrame(1, protocol.Request{Method: protocol.MethodGET, Path: "/a"}, 0)
	req2, _ := protocol.BuildRequestFrame(3, protocol.Request{Method: protocol.MethodGET, Path: "/b"}, 0)

	unknownHdr := make([]byte, 16)
	unknownHdr[0], unknownHdr[1], unknownHdr[2] = 0x42, 0x50, 0x01
	unknownHdr[3] = 0x7f // unknown type
	binary.BigEndian.PutUint32(unknownHdr[8:12], 4)
	binary.BigEndian.PutUint32(unknownHdr[12:16], 2)
	unknown := append(unknownHdr, []byte("SKIP")...)

	stream := append(append(req1, unknown...), req2...)
	r := bytes.NewReader(stream)

	// Frame 1
	h1, p1, err := protocol.ReadFrame(r)
	if err != nil || h1.Type != protocol.FrameTypeRequest {
		t.Fatalf("frame1: %v %+v", err, h1)
	}
	d1, _ := protocol.DecodeRequestPayload(p1, h1.HeaderCount)
	if d1.Path != "/a" {
		t.Fatal(d1.Path)
	}

	// Unknown — read and discard
	hU, pU, err := protocol.ReadFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	if protocol.IsKnownFrameType(hU.Type) {
		t.Fatal("expected unknown")
	}
	if len(pU) != 4 {
		t.Fatalf("payload len %d", len(pU))
	}

	// Frame 3
	h3, p3, err := protocol.ReadFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	d3, _ := protocol.DecodeRequestPayload(p3, h3.HeaderCount)
	if d3.Path != "/b" {
		t.Fatal(d3.Path)
	}
	if _, err := r.ReadByte(); err != io.EOF {
		t.Fatal("expected EOF")
	}
}

func TestPartialIOWriteRead(t *testing.T) {
	req := protocol.Request{Method: protocol.MethodGET, Path: "/partial"}
	frame, err := protocol.BuildRequestFrame(9, req, 0)
	if err != nil {
		t.Fatal(err)
	}
	// One-byte-at-a-time reader/writer.
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < len(frame); i++ {
			_, _ = pw.Write(frame[i : i+1])
		}
		_ = pw.Close()
	}()
	h, payload, err := protocol.ReadFrame(pr)
	if err != nil {
		t.Fatal(err)
	}
	got, err := protocol.DecodeRequestPayload(payload, h.HeaderCount)
	if err != nil || got.Path != "/partial" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestMultipleHeaders(t *testing.T) {
	headers := []protocol.Header{
		protocol.MakeHeader(protocol.HdrHost, "h"),
		protocol.MakeHeader(protocol.HdrAccept, "*/*"),
		protocol.MakeHeader(protocol.HdrConnection, "keep-alive"),
		protocol.MakeHeader(protocol.HdrUserAgent, "ua"),
		protocol.MakeHeader(protocol.HdrContentType, "text/plain"),
	}
	enc, err := protocol.EncodeHeaders(headers)
	if err != nil {
		t.Fatal(err)
	}
	dec, rest, err := protocol.DecodeHeaders(enc, uint16(len(headers)))
	if err != nil || len(rest) != 0 || len(dec) != 5 {
		t.Fatalf("%v %d rest=%d", err, len(dec), len(rest))
	}
}
