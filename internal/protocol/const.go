// Package protocol implements the BP/1 binary framing protocol.
package protocol

// Wire constants (network byte order for multi-byte fields).
const (
	Magic0   byte = 0x42 // 'B'
	Magic1   byte = 0x50 // 'P'
	Version1 byte = 0x01

	FrameHeaderSize = 16

	FrameTypeRequest  byte = 0x01
	FrameTypeResponse byte = 0x02

	FlagNone byte = 0x00

	MethodGET byte = 0x01

	StatusOK                  uint16 = 200
	StatusBadRequest          uint16 = 400
	StatusNotFound            uint16 = 404
	StatusInternalServerError uint16 = 500
)

// Protocol limits — reject attacker-controlled unbounded sizes.
const (
	MaxFramePayload      = 16 * 1024 * 1024 // 16 MiB
	MaxPathLength        = 2048
	MaxHeaderValueLength = 8192
	MaxHeaderCount       = 32
)

// Header dictionary IDs (compact names on the wire).
const (
	HdrHost            byte = 0
	HdrContentLength   byte = 1
	HdrContentType     byte = 2
	HdrConnection      byte = 3
	HdrUserAgent       byte = 4
	HdrAccept          byte = 5
	HdrServer          byte = 6
	HdrDate            byte = 7
	HdrCacheControl    byte = 8
	HdrContentEncoding byte = 9
)

// HeaderNames maps dictionary IDs to canonical names.
var HeaderNames = map[byte]string{
	HdrHost:            "Host",
	HdrContentLength:   "Content-Length",
	HdrContentType:     "Content-Type",
	HdrConnection:      "Connection",
	HdrUserAgent:       "User-Agent",
	HdrAccept:          "Accept",
	HdrServer:          "Server",
	HdrDate:            "Date",
	HdrCacheControl:    "Cache-Control",
	HdrContentEncoding: "Content-Encoding",
}

// HeaderIDByName maps canonical names to dictionary IDs.
var HeaderIDByName = map[string]byte{
	"Host":             HdrHost,
	"Content-Length":   HdrContentLength,
	"Content-Type":     HdrContentType,
	"Connection":       HdrConnection,
	"User-Agent":       HdrUserAgent,
	"Accept":           HdrAccept,
	"Server":           HdrServer,
	"Date":             HdrDate,
	"Cache-Control":    HdrCacheControl,
	"Content-Encoding": HdrContentEncoding,
}
