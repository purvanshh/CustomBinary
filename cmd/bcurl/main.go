// Command bcurl is the BP/1 client.
//
//	./bcurl [-v] <host>:<port>/<path>
package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"bp/internal/hexdump"
	"bp/internal/protocol"
)

func main() {
	verbose := false
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "-v" {
		verbose = true
		args = args[1:]
	}
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [-v] <host>:<port>/<path>\n", os.Args[0])
		os.Exit(2)
	}

	host, port, path, err := parseTarget(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid target: %v\n", err)
		os.Exit(2)
	}

	addr := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	req := protocol.Request{
		Method: protocol.MethodGET,
		Path:   path,
		Headers: []protocol.Header{
			protocol.MakeHeader(protocol.HdrHost, host),
			protocol.MakeHeader(protocol.HdrUserAgent, "bcurl/1.0"),
			protocol.MakeHeader(protocol.HdrConnection, "keep-alive"),
			protocol.MakeHeader(protocol.HdrAccept, "*/*"),
		},
	}

	frame, err := protocol.BuildRequestFrame(1, req, protocol.FlagNone)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}

	if verbose {
		hexdump.Dump(os.Stderr, "> REQUEST FRAME", frame)
	}

	if err := protocol.WriteAll(conn, frame); err != nil {
		fmt.Fprintf(os.Stderr, "send: %v\n", err)
		os.Exit(1)
	}

	// Read response, skipping any unknown frames (should not normally appear).
	var resp protocol.Response
	var rawFrame []byte
	for {
		h, payload, err := protocol.ReadFrame(conn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "recv: %v\n", err)
			os.Exit(1)
		}

		raw := make([]byte, protocol.FrameHeaderSize+len(payload))
		protocol.EncodeFrameHeader(h, raw[:protocol.FrameHeaderSize])
		copy(raw[protocol.FrameHeaderSize:], payload)

		if !protocol.IsKnownFrameType(h.Type) {
			if verbose {
				hexdump.Dump(os.Stderr, fmt.Sprintf("< UNKNOWN FRAME type=0x%02x (skipped)", h.Type), raw)
			}
			continue
		}
		if h.Type != protocol.FrameTypeResponse {
			fmt.Fprintf(os.Stderr, "unexpected frame type 0x%02x\n", h.Type)
			os.Exit(1)
		}
		resp, err = protocol.DecodeResponsePayload(payload, h.HeaderCount)
		if err != nil {
			fmt.Fprintf(os.Stderr, "decode response: %v\n", err)
			os.Exit(1)
		}
		rawFrame = raw
		break
	}

	if verbose {
		hexdump.Dump(os.Stderr, "< RESPONSE FRAME", rawFrame)
		fmt.Fprintf(os.Stderr, "status: %d\n", resp.Status)
		for _, h := range resp.Headers {
			name := protocol.HeaderNames[h.ID]
			fmt.Fprintf(os.Stderr, "header: %s: %s\n", name, string(h.Value))
		}
	}

	if _, err := os.Stdout.Write(resp.Body); err != nil {
		fmt.Fprintf(os.Stderr, "write body: %v\n", err)
		os.Exit(1)
	}

	if resp.Status >= 400 {
		os.Exit(1)
	}
	os.Exit(0)
}

// parseTarget parses host:port/path (path may be empty → "/").
func parseTarget(s string) (host, port, path string, err error) {
	// Split path first on first '/'.
	slash := strings.IndexByte(s, '/')
	var hp string
	if slash < 0 {
		hp = s
		path = "/"
	} else {
		hp = s[:slash]
		path = s[slash:]
		if path == "" {
			path = "/"
		}
	}
	if hp == "" {
		return "", "", "", fmt.Errorf("missing host:port")
	}

	// host:port — support IPv6 in [addr]:port
	if strings.HasPrefix(hp, "[") {
		h, p, e := net.SplitHostPort(hp)
		if e != nil {
			return "", "", "", e
		}
		host, port = h, p
	} else {
		colon := strings.LastIndexByte(hp, ':')
		if colon < 0 {
			return "", "", "", fmt.Errorf("missing port")
		}
		host = hp[:colon]
		port = hp[colon+1:]
	}
	if host == "" {
		host = "localhost"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", "", fmt.Errorf("bad port: %s", port)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return host, port, path, nil
}
