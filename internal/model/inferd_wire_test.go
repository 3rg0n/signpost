package model

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	payloads := []string{
		"",
		"{}",
		`{"type":"done","usage":{"input_tokens":1,"output_tokens":2}}`,
		strings.Repeat("x", 300),   // two length bytes
		strings.Repeat("y", 20000), // three length bytes
	}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	for _, p := range payloads {
		if err := writeFrame(w, frameJSON, []byte(p)); err != nil {
			t.Fatalf("writeFrame(%d bytes): %v", len(p), err)
		}
	}

	r := bufio.NewReader(&buf)
	for _, want := range payloads {
		typ, got, err := readFrame(r)
		if err != nil {
			t.Fatalf("readFrame: %v", err)
		}
		if typ != frameJSON {
			t.Errorf("type = 0x%02x, want 0x%02x", typ, frameJSON)
		}
		if string(got) != want {
			t.Errorf("payload = %d bytes, want %d", len(got), len(want))
		}
	}
	if _, _, err := readFrame(r); !errors.Is(err, errCleanClose) {
		t.Errorf("after the last frame err = %v, want errCleanClose", err)
	}
}

// The length prefix counts the payload alone, not the type byte. Off by one here
// desynchronises every subsequent frame, and the daemon would read signpost's type byte
// as the next frame's length — so it is asserted against hand-built bytes rather than
// against writeFrame's own output.
func TestWriteFrameLengthExcludesTypeByte(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := writeFrame(w, frameJSON, []byte("{}")); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	if want := []byte{0x02, frameJSON, '{', '}'}; !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("frame = % x, want % x", buf.Bytes(), want)
	}
}

// The cap is checked against the announced length before the payload is allocated, so a
// peer claiming a huge frame costs nothing. Verified by sending only the header: if the
// check happened after the read, this would block or allocate.
func TestReadFrameRejectsOversizedAnnouncementWithoutAllocating(t *testing.T) {
	var hdr [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(hdr[:], maxFrameBytes+1)
	r := bufio.NewReader(bytes.NewReader(append(hdr[:n], frameJSON)))

	_, _, err := readFrame(r)
	if err == nil {
		t.Fatal("an over-cap frame length was accepted")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Errorf("err = %v, want it to name the cap", err)
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	w := bufio.NewWriter(&bytes.Buffer{})
	err := writeFrame(w, frameJSON, make([]byte, maxFrameBytes+1))
	if err == nil {
		t.Fatal("an over-cap payload was written")
	}
}

// A varint that never terminates is a peer sending 0x80 forever. The spec caps a length
// at five bytes, so the sixth continuation is malformed rather than an infinite read.
func TestReadUvarintStopsAtFiveBytes(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader(bytes.Repeat([]byte{0x80}, 64)))
	_, err := readUvarint(r)
	if err == nil {
		t.Fatal("a non-terminating varint was accepted")
	}
	if !strings.Contains(err.Error(), "5 bytes") {
		t.Errorf("err = %v, want it to name the 5-byte limit", err)
	}
}

// Clean close and truncated frame mean opposite things: the first is a peer that
// finished, the second is a byte stream that can no longer be trusted. Conflating them
// would report a mid-frame disconnect as a normal end of stream.
func TestReadFrameDistinguishesCleanCloseFromTruncation(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		clean bool
	}{
		{"nothing at all", nil, true},
		{"length then EOF", []byte{0x05}, false},
		{"header then EOF", []byte{0x05, frameJSON}, false},
		{"partial payload", []byte{0x05, frameJSON, 'a', 'b'}, false},
		{"mid-varint EOF", []byte{0x80}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewReader(tc.bytes))
			_, _, err := readFrame(r)
			if err == nil {
				t.Fatal("a short stream produced no error")
			}
			if got := errors.Is(err, errCleanClose); got != tc.clean {
				t.Errorf("errors.Is(err, errCleanClose) = %v, want %v (err: %v)",
					got, tc.clean, err)
			}
			if !tc.clean && !strings.Contains(err.Error(), "truncated") {
				t.Errorf("err = %v, want it to say the frame was truncated", err)
			}
		})
	}
}
