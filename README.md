# BP/1 — Binary Protocol Client & Server

Custom binary HTTP-like protocol (**BP/1**) over TCP. See **[SPEC.md](SPEC.md)** for the authoritative wire format. See **[docs/annotated-hexdump.md](docs/annotated-hexdump.md)** for a real captured request/response.

## Overview

| Binary | Role |
|--------|------|
| `bserve` | Document-root file server |
| `bcurl`  | Client (one TCP connection, body to stdout) |

Protocol highlights: 16-byte big-endian frames (`Magic=BP`), GET requests, dictionary headers, persistent connections, unknown-frame skipping, path-traversal protection.

## Build

Requires Go 1.21+.

```bash
make
# or: go build -o bserve ./cmd/bserve && go build -o bcurl ./cmd/bcurl
```

## Server

```bash
./bserve ./www 9000
```

## Client

```bash
./bcurl localhost:9000/index.html
./bcurl -v localhost:9000/hello.txt    # hex dumps of frames on stderr
```

Exit status: `0` for status &lt; 400; non-zero for 4xx/5xx (and errors).

## Tests

```bash
make test
# or: go test ./...
```

## Example

```bash
./bserve ./www 9000 &
./bcurl localhost:9000/index.html
# → HTML body on stdout

./bcurl localhost:9000/missing.txt
# → non-zero exit, 404 body
```

## Layout

```
cmd/bserve/          server CLI
cmd/bcurl/           client CLI
internal/protocol/   encode/decode + constants
internal/iox/        exact-length I/O helpers
internal/pathsafe/   document-root path resolution
internal/hexdump/    verbose dumps
integration/         end-to-end tests
www/                 sample document root
SPEC.md              protocol specification
docs/annotated-hexdump.md
```
