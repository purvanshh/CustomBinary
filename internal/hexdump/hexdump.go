// Package hexdump formats binary frames for verbose (-v) debugging.
package hexdump

import (
	"fmt"
	"io"
	"strings"
)

// Dump writes a labeled hexadecimal dump with offsets and ASCII sidecar.
func Dump(w io.Writer, label string, data []byte) {
	fmt.Fprintf(w, "%s\n", label)
	const width = 16
	for i := 0; i < len(data); i += width {
		end := i + width
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i:end]

		var hexParts []string
		var ascii strings.Builder
		for j := 0; j < width; j++ {
			if j < len(chunk) {
				hexParts = append(hexParts, fmt.Sprintf("%02x", chunk[j]))
				c := chunk[j]
				if c >= 0x20 && c < 0x7f {
					ascii.WriteByte(c)
				} else {
					ascii.WriteByte('.')
				}
			} else {
				hexParts = append(hexParts, "  ")
			}
		}
		fmt.Fprintf(w, "%04x: %-47s  %s\n", i, strings.Join(hexParts, " "), ascii.String())
	}
	fmt.Fprintln(w)
}
