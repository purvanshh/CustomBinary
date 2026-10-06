package integration_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"bp/internal/protocol"
)

func buildBinaries(t *testing.T) (bserve, bcurl string) {
	t.Helper()
	dir := t.TempDir()
	bserve = filepath.Join(dir, "bserve")
	bcurl = filepath.Join(dir, "bcurl")
	modRoot := findModuleRoot(t)

	cmd := exec.Command("go", "build", "-o", bserve, "./cmd/bserve")
	cmd.Dir = modRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build bserve: %v\n%s", err, out)
	}
	cmd = exec.Command("go", "build", "-o", bcurl, "./cmd/bcurl")
	cmd.Dir = modRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build bcurl: %v\n%s", err, out)
	}
	return bserve, bcurl
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func startServer(t *testing.T, bserve, root string) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cmd := exec.Command(bserve, root, fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	addr = fmt.Sprintf("127.0.0.1:%d", port)

	// Wait until accepting.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	stop = func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return addr, stop
}

func TestHTTPLikeBehavior(t *testing.T) {
	bserve, bcurl := buildBinaries(t)
	www := t.TempDir()
	_ = os.WriteFile(filepath.Join(www, "index.html"), []byte("<h1>hi</h1>"), 0644)
	_ = os.WriteFile(filepath.Join(www, "hello.txt"), []byte("hello"), 0644)

	addr, stop := startServer(t, bserve, www)
	defer stop()

	out, err := exec.Command(bcurl, addr+"/index.html").CombinedOutput()
	if err != nil {
		t.Fatalf("200 case: %v\n%s", err, out)
	}
	if !bytes.Equal(out, []byte("<h1>hi</h1>")) {
		t.Fatalf("body=%q", out)
	}

	cmd := exec.Command(bcurl, addr+"/missing.txt")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for 404, body=%q", out)
	}
}

func TestPersistentConnection(t *testing.T) {
	bserve, _ := buildBinaries(t)
	www := t.TempDir()
	_ = os.WriteFile(filepath.Join(www, "index.html"), []byte("IDX"), 0644)
	_ = os.WriteFile(filepath.Join(www, "hello.txt"), []byte("HELLO"), 0644)

	addr, stop := startServer(t, bserve, www)
	defer stop()

	// Exactly ONE Dial for the entire request sequence.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	local0 := conn.LocalAddr().String()
	remote0 := conn.RemoteAddr().String()

	paths := []string{"/index.html", "/hello.txt", "/missing.txt"}
	wantStatus := []uint16{200, 200, 404}
	wantBody := []string{"IDX", "HELLO", "not found"}

	for i, p := range paths {
		// Prove we never replaced the TCP connection mid-sequence.
		if conn.LocalAddr().String() != local0 || conn.RemoteAddr().String() != remote0 {
			t.Fatalf("TCP endpoints changed after request %d — connection was replaced", i)
		}

		req := protocol.Request{Method: protocol.MethodGET, Path: p}
		frame, err := protocol.BuildRequestFrame(uint32(i+1), req, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := protocol.WriteAll(conn, frame); err != nil {
			t.Fatal(err)
		}
		h, payload, err := protocol.ReadFrame(conn)
		if err != nil {
			t.Fatal(err)
		}
		if h.Type != protocol.FrameTypeResponse {
			t.Fatalf("type %d", h.Type)
		}
		resp, err := protocol.DecodeResponsePayload(payload, h.HeaderCount)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Status != wantStatus[i] {
			t.Fatalf("req %d status %d want %d", i, resp.Status, wantStatus[i])
		}
		if string(resp.Body) != wantBody[i] {
			t.Fatalf("req %d body %q want %q", i, resp.Body, wantBody[i])
		}
	}

	// Same socket still usable after the third exchange (not closed by server).
	if conn.LocalAddr().String() != local0 {
		t.Fatal("connection closed/replaced after sequence")
	}
}

func TestMalformedRequestReturns400(t *testing.T) {
	bserve, _ := buildBinaries(t)
	www := t.TempDir()
	addr, stop := startServer(t, bserve, www)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Valid header but garbage payload for REQUEST.
	hdr := make([]byte, 16)
	hdr[0], hdr[1], hdr[2], hdr[3] = 0x42, 0x50, 0x01, 0x01
	binary.BigEndian.PutUint16(hdr[6:8], 0)
	binary.BigEndian.PutUint32(hdr[8:12], 3)
	binary.BigEndian.PutUint32(hdr[12:16], 99)
	payload := []byte{0xff, 0x00, 0x00} // bad method
	if err := protocol.WriteAll(conn, append(hdr, payload...)); err != nil {
		t.Fatal(err)
	}
	h, pl, err := protocol.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.DecodeResponsePayload(pl, h.HeaderCount)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 400 {
		t.Fatalf("status=%d", resp.Status)
	}
}

func TestUnknownFrameThenKnown(t *testing.T) {
	bserve, _ := buildBinaries(t)
	www := t.TempDir()
	_ = os.WriteFile(filepath.Join(www, "a.txt"), []byte("AAA"), 0644)
	_ = os.WriteFile(filepath.Join(www, "b.txt"), []byte("BBB"), 0644)
	addr, stop := startServer(t, bserve, www)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	writeUnknown := func(payload []byte) {
		t.Helper()
		uh := make([]byte, 16)
		uh[0], uh[1], uh[2] = 0x42, 0x50, 0x01
		uh[3] = 0x55 // unknown type
		binary.BigEndian.PutUint32(uh[8:12], uint32(len(payload)))
		binary.BigEndian.PutUint32(uh[12:16], 99)
		if err := protocol.WriteAll(conn, append(uh, payload...)); err != nil {
			t.Fatal(err)
		}
	}
	doGET := func(id uint32, path, wantBody string) {
		t.Helper()
		frame, err := protocol.BuildRequestFrame(id, protocol.Request{Method: protocol.MethodGET, Path: path}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := protocol.WriteAll(conn, frame); err != nil {
			t.Fatal(err)
		}
		h, payload, err := protocol.ReadFrame(conn)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := protocol.DecodeResponsePayload(payload, h.HeaderCount)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Status != 200 || string(resp.Body) != wantBody {
			t.Fatalf("id=%d got status=%d body=%q want %q", id, resp.Status, resp.Body, wantBody)
		}
	}

	// Sandwich: known → unknown → known on the SAME connection.
	// Proves: read header → unknown → read/discard exactly PayloadLen → continue → next frame OK.
	doGET(1, "/a.txt", "AAA")
	writeUnknown([]byte("SKIPME")) // PayloadLen=6, must be fully discarded
	doGET(2, "/b.txt", "BBB")
}

func TestPathTraversal(t *testing.T) {
	bserve, bcurl := buildBinaries(t)
	parent := t.TempDir()
	www := filepath.Join(parent, "www")
	_ = os.Mkdir(www, 0755)
	_ = os.WriteFile(filepath.Join(parent, "secret"), []byte("LEAK"), 0644)
	_ = os.WriteFile(filepath.Join(www, "safe.txt"), []byte("safe"), 0644)

	addr, stop := startServer(t, bserve, www)
	defer stop()

	for _, p := range []string{"/../../secret", "/../secret", "/foo/../../secret"} {
		out, err := exec.Command(bcurl, addr+p).CombinedOutput()
		if bytes.Contains(out, []byte("LEAK")) {
			t.Fatalf("path %s leaked: %q err=%v", p, out, err)
		}
	}
}

func TestVerboseHexdump(t *testing.T) {
	bserve, bcurl := buildBinaries(t)
	www := t.TempDir()
	_ = os.WriteFile(filepath.Join(www, "index.html"), []byte("X"), 0644)
	addr, stop := startServer(t, bserve, www)
	defer stop()

	cmd := exec.Command(bcurl, "-v", addr+"/index.html")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, stderr.Bytes())
	}
	s := stderr.String()
	if !bytes.Contains(stderr.Bytes(), []byte("> REQUEST FRAME")) {
		t.Fatalf("missing request dump: %s", s)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("< RESPONSE FRAME")) {
		t.Fatalf("missing response dump: %s", s)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("42 50")) {
		t.Fatalf("missing magic in dump: %s", s)
	}
}

func TestLargeFilePartialIO(t *testing.T) {
	bserve, bcurl := buildBinaries(t)
	www := t.TempDir()
	big := bytes.Repeat([]byte("ABCDEFGH"), 32*1024) // 256 KiB
	_ = os.WriteFile(filepath.Join(www, "big.bin"), big, 0644)
	addr, stop := startServer(t, bserve, www)
	defer stop()

	out, err := exec.Command(bcurl, addr+"/big.bin").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, big) {
		t.Fatalf("len got=%d want=%d", len(out), len(big))
	}
}
