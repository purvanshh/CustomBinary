# BP/1 Protocol Specification

**Binary Protocol version 1 (BP/1)** — a compact HTTP-like request/response protocol over TCP.

## 1. Overview

BP/1 carries resource requests (GET) and responses (status, headers, body) as **binary frames** on a persistent TCP connection. It is not HTTP and does not use textual request lines.

A peer that implements only this document must interoperate with any compliant client or server.

## 2. Transport

- Transport: **TCP**
- One logical session = one TCP connection
- Multiple request/response exchanges MAY reuse the same connection (persistent)
- Framing is self-delimiting; message boundaries are defined by the frame header’s Payload Length

## 3. Frame Layout

Every frame is:

```
[ Fixed 16-byte Header ][ Payload of exactly Payload Length bytes ]
```

### 3.1 Header (16 bytes, big-endian)

| Offset | Size | Field           | Notes                          |
|-------:|-----:|-----------------|--------------------------------|
| 0      | 2    | Magic           | `0x42 0x50` (“BP”)             |
| 2      | 1    | Version         | `0x01`                         |
| 3      | 1    | Frame Type      | see §3.3                       |
| 4      | 1    | Flags           | `0x00` for this assignment     |
| 5      | 1    | Reserved        | MUST be `0x00`                 |
| 6      | 2    | Header Count    | number of header entries       |
| 8      | 4    | Payload Length  | bytes following this header    |
| 12     | 4    | Request ID      | correlates request & response  |

**Endianness:** all multi-byte integers are **network byte order (big-endian)**.

### 3.2 Magic and Version

- Magic: `0x42 0x50`
- Version: `0x01`
- Receivers MUST reject other magic or version as malformed

### 3.3 Frame Types

| Value  | Name     |
|--------|----------|
| `0x01` | REQUEST  |
| `0x02` | RESPONSE |

Other values are **reserved for future use**.

### 3.4 Flags

No flag bits are defined in BP/1.

| Rule | Requirement |
|------|-------------|
| Sender | MUST set Flags to `0x00` |
| Receiver | MUST accept `0x00` and MUST ignore the Flags byte (do not reject a frame solely because Flags ≠ 0) |

Future versions MAY assign meaning to individual bits; BP/1 peers treat Flags as reserved-for-extension padding.

### 3.5 Unknown Frame Types (mandatory)

**A receiver that encounters an unknown frame type MUST consume and discard exactly Payload Length bytes and continue processing subsequent frames.**

Required receive algorithm for every frame:

```
1. Read exactly 16 bytes → fixed header
2. Validate Magic, Version, Reserved, Payload Length limit, Header Count limit
3. Read exactly Payload Length bytes → payload
4. If Frame Type is unknown:
      discard the payload (do not interpret it)
      go to step 1 for the next frame
5. Otherwise decode the payload according to Frame Type
```

The receiver MUST NOT crash solely because the frame type is unknown.
After discarding the payload, parsing resumes at the next 16-byte header.
Header Count is still validated against the limit for all frames (including unknown),
but unknown payloads are never interpreted as REQUEST/RESPONSE.

## 4. Limits

| Constant               | Value        |
|------------------------|-------------:|
| `MAX_FRAME_PAYLOAD`    | 16 777 216 (16 MiB) |
| `MAX_PATH_LENGTH`      | 2048         |
| `MAX_HEADER_VALUE_LENGTH` | 8192      |
| `MAX_HEADER_COUNT`     | 32           |

Frames exceeding these limits are malformed.

## 5. Header Dictionary

Header names are encoded as a 1-byte ID (not as free-form strings):

| ID | Name              |
|---:|-------------------|
| 0  | Host              |
| 1  | Content-Length    |
| 2  | Content-Type      |
| 3  | Connection        |
| 4  | User-Agent        |
| 5  | Accept            |
| 6  | Server            |
| 7  | Date              |
| 8  | Cache-Control     |
| 9  | Content-Encoding  |

### 5.1 Header Encoding

Each header entry is a self-delimited record:

| Offset (within entry) | Size | Field | Encoding / constraints |
|----------------------:|-----:|-------|------------------------|
| 0 | 1 | Header ID | Dictionary ID 0–9 (§5); other IDs are malformed in BP/1 |
| 1 | 2 | Value Length | uint16 BE; MUST be ≤ `MAX_HEADER_VALUE_LENGTH` |
| 3 | Value Length | Value | UTF-8/ASCII bytes; not NUL-terminated |

Header Count in the frame header equals the number of such entries present in the payload (REQUEST: after Path; RESPONSE: after Status, before Body).

## 6. REQUEST Payload (`Frame Type = 0x01`)

Layout of the payload bytes (length = Payload Length):

```
Offset (within payload)   Size        Field
-----------------------   ----        -----
0                         1           Method
1                         2           Path Length (uint16 BE)
3                         Path Length Path (UTF-8/ASCII bytes)
3+Path Length             variable    Headers (exactly Header Count entries)
```

| Field | Encoding | Allowed values / semantics |
|-------|----------|----------------------------|
| Method | 1 byte | `0x01` = GET (only method required for this assignment) |
| Path Length | uint16 BE | 0 … `MAX_PATH_LENGTH`; number of Path bytes that follow |
| Path | Path Length bytes | Resource path, typically starting with `/` (e.g. `/index.html`) |
| Headers | §5.1 × Header Count | Dictionary headers; Header Count comes from the **frame header** |

Decode algorithm:

```
read Method
read Path Length
read Path Length bytes → Path
for i in 1 .. Header Count:
    read header entry (§5.1)
if any bytes remain in the payload → malformed
```

Payload Length MUST equal the size of the above fields with **no trailing junk**.

Example conceptual mapping: textual `GET /index.html` becomes Method=`0x01`, Path=`/index.html`, plus optional dictionary headers — **not** an HTTP text line.

## 7. RESPONSE Payload (`Frame Type = 0x02`)

Layout of the payload bytes (length = Payload Length):

```
Offset (within payload)   Size        Field
-----------------------   ----        -----
0                         2           Status Code (uint16 BE)
2                         variable    Headers (exactly Header Count entries)
2+headers_bytes           remaining   Body
```

### 7.0 Header / body boundary (normative)

The boundary between headers and body is defined **only** by Header Count from the frame header:

```
16-byte frame header
    ↓
HeaderCount tells the decoder how many header entries follow the status
    ↓
Status Code (2 bytes)
    ↓
exactly HeaderCount header entries (§5.1)
    ↓
all remaining payload bytes = Body
```

There is no separate body-length field in the payload. Body length is:

```
BodyLength = Payload Length − 2 − (size of the HeaderCount header entries)
```

If a Content-Length header is present, its decimal value SHOULD equal BodyLength.
Receivers MAY use Content-Length as advisory metadata; the on-the-wire body is still “remaining payload bytes after headers.”

Decode algorithm:

```
read Status Code (2 bytes BE)
for i in 1 .. Header Count:
    read header entry (§5.1)
remaining payload bytes → Body (may be empty)
```

| Field | Encoding | Semantics |
|-------|----------|-----------|
| Status Code | uint16 BE | see §7.1 |
| Headers | §5.1 × Header Count | response metadata |
| Body | 0 or more bytes | raw file bytes on success; short error text is allowed |

### 7.1 Status Codes

| Code | Meaning                |
|-----:|------------------------|
| 200  | OK                     |
| 400  | Bad Request            |
| 404  | Not Found              |
| 500  | Internal Server Error  |

Successful file responses SHOULD include at least:

- Content-Length
- Content-Type
- Connection (`keep-alive`)

## 8. Persistent Connections

After accepting a TCP connection, a server MUST:

```
while connection alive:
    read one frame
    if unknown type: discard payload; continue
    if REQUEST: process; write one RESPONSE frame
    keep connection open
```

Clients SHOULD open **one** TCP connection per invocation for a single request/response (or a planned sequence) and MUST NOT open a second connection for that sequence.

Neither side may assume a single `recv`/`read` or `send`/`write` transfers an entire frame. Implementations MUST use exact-length I/O loops.

## 9. Error Handling

- Bad magic, bad version, non-zero Reserved (where required), oversized lengths, truncated headers/payloads, invalid path/header lengths, unsupported methods, unknown header IDs → **malformed**
- Servers SHOULD respond with status **400** when a REQUEST is malformed but the connection remains usable
- Severely corrupted streams (e.g. cannot resynchronize) MAY close the connection
- Missing file → **404**
- Unexpected I/O/filesystem failures → **500**

## 10. Path Handling and Security

Server CLI: `./bserve <document-root> <port>`

Request path `/index.html` maps under the document root (e.g. `./www/index.html`).

The server MUST canonicalize/normalize paths and **reject or neutralize** traversal such that files outside the document root are never served. Examples that must not escape the root:

- `/../../etc/passwd`
- `/../secret`
- `/foo/../../secret`

Symlinks that resolve outside the document root MUST also be refused.

## 11. Versioning / Extensibility

- Version field allows future BP/n layouts
- Unknown frame types are skipped per §3.5 without breaking the session
- New header IDs may be added in future versions; BP/1 receivers treat unknown IDs as malformed
- Flags are reserved for future optional features

## 12. Interoperability Rule

The **wire format** is the only contract. Clients and servers MUST NOT rely on shared in-memory types or undocumented side channels. Every field needed to decode a frame appears in the header or payload as specified above.
