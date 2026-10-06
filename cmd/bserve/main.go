// Command bserve is the BP/1 document server.
//
//	./bserve <document-root> <port>
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bp/internal/iox"
	"bp/internal/pathsafe"
	"bp/internal/protocol"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <document-root> <port>\n", os.Args[0])
		os.Exit(2)
	}
	root := os.Args[1]
	port := os.Args[2]

	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		log.Fatalf("document root %q is not a directory: %v", root, err)
	}

	addr := ":" + port
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("bserve listening on %s root=%s", addr, root)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handleConn(conn, root)
	}
}

func handleConn(conn net.Conn, root string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Time{}) // no global deadline; persistent

	for {
		// Idle clients may hang forever; optional soft read deadline per frame header.
		_ = iox.SetRWDeadline(conn, 5*time.Minute)

		hdrBuf, err := protocol.ReadExact(conn, protocol.FrameHeaderSize)
		if err != nil {
			if errors.Is(err, protocol.ErrTruncated) || errors.Is(err, io.EOF) {
				return // peer closed
			}
			log.Printf("read header: %v", err)
			return
		}

		h, err := protocol.DecodeFrameHeader(hdrBuf)
		if err != nil {
			// Malformed header: try to send 400 if we can guess nothing; otherwise close.
			log.Printf("malformed header: %v", err)
			_ = sendError(conn, 0, protocol.StatusBadRequest)
			return
		}

		payload, err := protocol.ReadExact(conn, int(h.PayloadLength))
		if err != nil {
			log.Printf("read payload: %v", err)
			return
		}

		if !protocol.IsKnownFrameType(h.Type) {
			// SPEC: consume Payload Length bytes (already read) and continue.
			log.Printf("skipping unknown frame type 0x%02x len=%d", h.Type, h.PayloadLength)
			continue
		}

		if h.Type != protocol.FrameTypeRequest {
			// Known but not a request (e.g. RESPONSE sent to server): 400.
			_ = sendError(conn, h.RequestID, protocol.StatusBadRequest)
			continue
		}

		req, err := protocol.DecodeRequestPayload(payload, h.HeaderCount)
		if err != nil {
			log.Printf("bad request: %v", err)
			_ = sendError(conn, h.RequestID, protocol.StatusBadRequest)
			continue
		}

		resp := serveFile(root, req)
		frame, err := protocol.BuildResponseFrame(h.RequestID, resp, protocol.FlagNone)
		if err != nil {
			log.Printf("encode response: %v", err)
			_ = sendError(conn, h.RequestID, protocol.StatusInternalServerError)
			continue
		}
		if err := protocol.WriteAll(conn, frame); err != nil {
			log.Printf("write response: %v", err)
			return
		}
		// Connection stays open for the next request.
	}
}

func serveFile(root string, req protocol.Request) protocol.Response {
	fsPath, err := pathsafe.Resolve(root, req.Path)
	if err != nil {
		if errors.Is(err, pathsafe.ErrTraversal) || errors.Is(err, pathsafe.ErrEmptyPath) {
			return errorResponse(protocol.StatusBadRequest, "bad path")
		}
		return errorResponse(protocol.StatusBadRequest, "bad path")
	}

	data, err := os.ReadFile(fsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errorResponse(protocol.StatusNotFound, "not found")
		}
		// Also treat directories / permission as 404 or 500 appropriately.
		if fi, e2 := os.Stat(fsPath); e2 == nil && fi.IsDir() {
			return errorResponse(protocol.StatusNotFound, "not found")
		}
		return errorResponse(protocol.StatusInternalServerError, "read error")
	}

	ctype := mime.TypeByExtension(filepath.Ext(fsPath))
	if ctype == "" {
		ctype = "application/octet-stream"
	}

	return protocol.Response{
		Status: protocol.StatusOK,
		Headers: []protocol.Header{
			protocol.MakeHeader(protocol.HdrContentLength, strconv.Itoa(len(data))),
			protocol.MakeHeader(protocol.HdrContentType, ctype),
			protocol.MakeHeader(protocol.HdrConnection, "keep-alive"),
			protocol.MakeHeader(protocol.HdrServer, "bserve/1.0"),
		},
		Body: data,
	}
}

func errorResponse(status uint16, msg string) protocol.Response {
	body := []byte(msg)
	return protocol.Response{
		Status: status,
		Headers: []protocol.Header{
			protocol.MakeHeader(protocol.HdrContentLength, strconv.Itoa(len(body))),
			protocol.MakeHeader(protocol.HdrContentType, "text/plain"),
			protocol.MakeHeader(protocol.HdrConnection, "keep-alive"),
			protocol.MakeHeader(protocol.HdrServer, "bserve/1.0"),
		},
		Body: body,
	}
}

func sendError(conn net.Conn, reqID uint32, status uint16) error {
	resp := errorResponse(status, statusText(status))
	frame, err := protocol.BuildResponseFrame(reqID, resp, protocol.FlagNone)
	if err != nil {
		return err
	}
	return protocol.WriteAll(conn, frame)
}

func statusText(s uint16) string {
	switch s {
	case protocol.StatusOK:
		return "OK"
	case protocol.StatusBadRequest:
		return "Bad Request"
	case protocol.StatusNotFound:
		return "Not Found"
	case protocol.StatusInternalServerError:
		return "Internal Server Error"
	default:
		return strings.TrimSpace(fmt.Sprintf("status %d", s))
	}
}
