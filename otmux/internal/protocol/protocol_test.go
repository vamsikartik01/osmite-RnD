package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	frame, err := EncodeJSON(TypeHello, Hello{Version: Version, Workspace: "api", Cols: 120, Rows: 40})
	if err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypeHello {
		t.Fatalf("type = %d, want %d", f.Type, TypeHello)
	}
	var h Hello
	if err := f.Decode(&h); err != nil {
		t.Fatal(err)
	}
	if h.Workspace != "api" || h.Cols != 120 || h.Rows != 40 {
		t.Fatalf("got %+v", h)
	}
}

func TestOutputRoundTrip(t *testing.T) {
	data := []byte("\x1b[31mhello\x1b[0m")
	f, err := ReadFrame(bytes.NewReader(EncodeFrame(TypeOutput, EncodeOutput(7, data))))
	if err != nil {
		t.Fatal(err)
	}
	pane, got, err := DecodeOutput(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if pane != 7 || !bytes.Equal(got, data) {
		t.Fatalf("pane=%d data=%q", pane, got)
	}
}

func TestReadFrameRejectsBadLength(t *testing.T) {
	for _, n := range []uint32{0, MaxFrameSize + 1} {
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], n)
		if _, err := ReadFrame(bytes.NewReader(hdr[:])); err == nil {
			t.Fatalf("length %d: expected error", n)
		}
	}
}
