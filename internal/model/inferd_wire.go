package model

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The inferd v2 generation wire format, implemented against docs/protocol-v2.md in
// the inferd repository rather than against any existing client's source.
//
// That choice is the design's (§5) and it is worth restating where the code is: the
// protocol carries an in-band wire_version that fails loudly on mismatch, which is
// exactly the condition under which implementing to a published spec beats vendoring
// a client. It keeps signpost's release cadence independent of inferd's, and it keeps
// signpost's dependency list empty (ADR 0002).
//
// A frame is:
//
//	payload_len   uvarint (LEB128), payload bytes only, never the type byte
//	frame_type    1 byte — 0x01 JSON, 0x02 BLOB
//	payload       exactly payload_len bytes
//
// signpost sends and receives only JSON frames: attachments carry decoded media, and
// signpost analyses source text.

const (
	// frameJSON is a control frame: request, response, or blob descriptor.
	frameJSON byte = 0x01
	// frameBlob is raw bytes. signpost never sends one, and a daemon that sends one
	// on the response stream is a protocol error, so the constant exists to name that
	// check rather than to be written.
	frameBlob byte = 0x02

	// maxFrameBytes is the protocol's hard cap (64 MiB, THREAT_MODEL F-5). It is
	// checked against the decoded length *before* any payload byte is read, so a
	// hostile or broken peer cannot induce a large allocation by claiming a large
	// frame.
	maxFrameBytes = 64 << 20

	// maxVarintBytes is the spec's stop condition: 64 MiB fits in 27 bits, so a
	// well-formed length is at most four groups and five is the hard stop. Without
	// this, a peer sending 0x80 forever is an infinite read.
	maxVarintBytes = 5

	// wireVersion is stamped on every request. A daemon that speaks a different
	// version replies with one terminal error and closes, which is the loud failure
	// the in-band version exists to produce.
	wireVersion = 1
)

// errCleanClose is EOF at a frame boundary: the peer finished and closed.
//
// Distinguished from a truncated frame because they mean opposite things. EOF before
// the first length byte is a peer that is done; EOF mid-varint, mid-type, or
// mid-payload means the byte stream is no longer trustworthy and the connection
// cannot be resynced.
var errCleanClose = errors.New("inferd: connection closed between frames")

// writeFrame writes one length-prefixed, type-tagged frame.
//
// Flushed per frame because the protocol requires it: consumers rely on per-frame
// visibility for streaming, and a buffered writer that holds a request until its
// buffer fills would deadlock against a daemon waiting to read it.
func writeFrame(w *bufio.Writer, typ byte, payload []byte) error {
	if len(payload) > maxFrameBytes {
		return fmt.Errorf("inferd: frame of %d bytes exceeds the %d-byte cap", len(payload), maxFrameBytes)
	}
	var hdr [maxVarintBytes]byte
	n := binary.PutUvarint(hdr[:], uint64(len(payload)))
	if _, err := w.Write(hdr[:n]); err != nil {
		return err
	}
	if err := w.WriteByte(typ); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return w.Flush()
}

// readFrame reads one frame, returning its type and payload.
func readFrame(r *bufio.Reader) (byte, []byte, error) {
	n, err := readUvarint(r)
	if err != nil {
		return 0, nil, err
	}
	if n > maxFrameBytes {
		return 0, nil, fmt.Errorf("inferd: peer announced a %d-byte frame, over the %d-byte cap", n, maxFrameBytes)
	}
	typ, err := r.ReadByte()
	if err != nil {
		return 0, nil, truncated("frame type", err)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, truncated("frame payload", err)
	}
	return typ, payload, nil
}

// readUvarint decodes a LEB128 length, stopping after maxVarintBytes.
//
// Hand-rolled rather than binary.ReadUvarint because that function reads up to ten
// bytes for a 64-bit value; the spec caps a length at five and treats a sixth
// continuation byte as malformed. Accepting a longer varint would accept frames the
// daemon would reject, which is a client that disagrees with the contract it claims to
// implement.
func readUvarint(r *bufio.Reader) (uint64, error) {
	var value uint64
	var shift uint
	for i := 0; i < maxVarintBytes; i++ {
		b, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && i == 0 {
				return 0, errCleanClose
			}
			return 0, truncated("frame length", err)
		}
		value |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return value, nil
		}
		shift += 7
	}
	return 0, errors.New("inferd: frame length varint did not terminate within 5 bytes")
}

// truncated reports a mid-frame read failure as the protocol violation it is.
func truncated(what string, err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("inferd: connection closed mid-%s, so the frame is truncated", what)
	}
	return fmt.Errorf("inferd: reading %s: %w", what, err)
}
