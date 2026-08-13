package main

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

// TestByteSizeParsesWhatAPersonWouldType covers the flag's own parsing, which the
// end-to-end test below cannot see: a budget is only ever wrong by a factor of 1024, and
// every such mistake produces a walk that succeeds and reads the wrong amount.
//
// The KB/KiB pairs asserting the same value are the deliberate part. Both spellings mean
// the binary multiple here, because somebody raising a memory budget who writes 2GB means
// two gigabytes as their machine reports them, and being correct about SI would hand them
// 7% less than they asked for without saying so.
func TestByteSizeParsesWhatAPersonWouldType(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"512MiB", 512 << 20},
		{"512MB", 512 << 20},
		{"512m", 512 << 20},
		{"2GiB", 2 << 30},
		{"2gib", 2 << 30},
		{"2G", 2 << 30},
		// A budget between two powers of two is the ordinary case on a machine with an
		// awkward amount of memory, so a fraction has to work.
		{"1.5GiB", 1<<30 + 512<<20},
		{"4TiB", 4 << 40},
		// No suffix is bytes, which is what a caller scripting this would compute.
		{"1048576", 1 << 20},
		{"1024B", 1 << 10},
		// Whitespace between the number and the unit, because a shell quote makes it easy.
		{"2 GiB", 2 << 30},
	} {
		var b byteSize
		if err := b.Set(tc.in); err != nil {
			t.Errorf("Set(%q): %v", tc.in, err)
			continue
		}
		if int64(b) != tc.want {
			t.Errorf("Set(%q) = %d, want %d", tc.in, int64(b), tc.want)
		}
	}
}

// A budget that cannot be honoured is rejected at parse time rather than clamped.
//
// Zero is the case that matters and the reason this is not folded into the table above:
// the library treats a zero MaxTotalBytes as "use the default", so a flag that accepted
// `-max-bytes 0` would silently give the caller the default they were trying to change.
// The mistake has to be reported where it was made.
func TestByteSizeRejectsWhatCannotBeABudget(t *testing.T) {
	for _, in := range []string{"", "0", "0MiB", "-1", "-2GiB", "lots", "MiB", "2XiB", "1e999"} {
		var b byteSize
		if err := b.Set(in); err == nil {
			t.Errorf("Set(%q) = %d, want an error", in, int64(b))
		}
	}
}

// TestMaxBytesReportsWhereTheWalkStopped is the wiring plus the message, which is the half
// that makes the flag findable: a user who does not know the budget exists learns about it
// from this line and nowhere else.
//
// Driven through the real CLI on a tree small enough that a tiny budget truncates it. The
// assertions are about what a reader needs — that the walk stopped, *where* it stopped, and
// the name of the flag that lifts it — rather than the exact sentence, because the wording
// is not a contract and the three facts are.
func TestMaxBytesReportsWhereTheWalkStopped(t *testing.T) {
	root := fixture(t)

	_, stderr, code := invoke(t, "graph", "show", "-max-bytes", "200B", root)
	if code != 0 {
		t.Fatalf("a truncated walk is not a failure: exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "byte budget ran out at") {
		t.Errorf("stderr does not say the walk stopped:\n%s", stderr)
	}
	if !strings.Contains(stderr, "-max-bytes") {
		t.Errorf("stderr does not name the flag that lifts the budget:\n%s", stderr)
	}
	// The default, rendered the way the flag accepts it, so it can be pasted back as a
	// starting point rather than converted from a byte count.
	if want := humanBytes(discover.DefaultMaxTotalBytes); !strings.Contains(stderr, want) {
		t.Errorf("stderr does not state the default %s:\n%s", want, stderr)
	}
	// A path, and specifically one from the fixture: the count alone is the thing that made
	// this unactionable in the report it came from. Traversal is sorted, so the first
	// skipped path bounds everything missing.
	if !strings.Contains(stderr, ".go") && !strings.Contains(stderr, ".ts") {
		t.Errorf("stderr names no path, so a reader cannot tell what is missing:\n%s", stderr)
	}

	// The same tree with the budget left alone: no warning at all. Asserted because a
	// message that appeared on every run would train people to ignore the one that matters.
	_, stderr, code = invoke(t, "graph", "show", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if strings.Contains(stderr, "byte budget") {
		t.Errorf("a walk within the budget must not mention it:\n%s", stderr)
	}
}

// humanBytes renders the default the way the flag accepts it. Asserted against the
// suffix table rather than eyeballed, because the message tells the reader to paste it
// back and a value the flag cannot parse would be worse than a raw byte count.
func TestHumanBytesRoundTripsThroughTheFlag(t *testing.T) {
	for _, n := range []int64{
		discover.DefaultMaxTotalBytes,
		1 << 10, 1 << 20, 2 << 30, 4 << 40,
		// Not a whole multiple of anything above bytes, so it must fall through to B
		// rather than round.
		1<<20 + 1,
	} {
		s := humanBytes(n)
		var b byteSize
		if err := b.Set(s); err != nil {
			t.Errorf("humanBytes(%d) = %q, which the flag rejects: %v", n, s, err)
			continue
		}
		if int64(b) != n {
			t.Errorf("humanBytes(%d) = %q, which parses back as %d", n, s, int64(b))
		}
	}
}
