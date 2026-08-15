package main

import (
	"os"
	"path/filepath"
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
	// this unactionable in the report it came from. Skips are appended in traversal order,
	// so the first one bounds everything missing.
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

// The default renders as a unit, not as a byte count.
//
// The round-trip above is satisfied by either: `3221225473B` parses back exactly, so a
// default one byte off a power of two would pass it and still put eleven digits in the one
// message that has to be read and acted on. That is the whole argument for the flag taking a
// unit in the first place — a number nobody can check by eye — and it applies to the default
// the message tells them to raise from.
//
// The negative boundary is what makes it a test rather than a comment: asserting the suffix
// is absent from a byte count, not merely present in a good rendering, so a humanBytes that
// appended "B" to everything fails here.
func TestTheDefaultBudgetRendersAsAUnit(t *testing.T) {
	got := humanBytes(discover.DefaultMaxTotalBytes)
	if strings.HasSuffix(got, "B") && !strings.HasSuffix(got, "iB") {
		t.Errorf("the default budget renders as %q, a raw byte count. It is quoted in the "+
			"warning that tells somebody to raise it, and a value they cannot check by eye is "+
			"the reason -max-bytes takes a unit at all. Pick a default that is a whole "+
			"multiple of a binary unit", got)
	}
}

// The usage line states the default and gives an example above it.
//
// Read before a run rather than after one, which is what distinguishes it from the warning:
// by the time the warning appears the walk has already been truncated. The example is the
// case that actually broke — it was a literal `2GiB` and the default grew past it, so the
// help suggested a budget smaller than the one already in force — and asserting the ordering
// rather than the number is what keeps it correct through the next raise.
func TestMaxBytesUsageStatesTheDefaultAndExceedsIt(t *testing.T) {
	// Stdout and exit 0: an explicit -h is a request that succeeded, not a usage error.
	usage, stderr, code := invoke(t, "graph", "show", "-h")
	if code != 0 {
		t.Fatalf("-h exit = %d\n%s", code, stderr)
	}
	i := strings.Index(usage, "-max-bytes")
	if i < 0 {
		t.Fatalf("no -max-bytes in usage:\n%s", usage)
	}
	// The flag's own entry only: its name line plus the indented description beneath it.
	// Cut at the next entry rather than at a named neighbour, so a reordered flag set cannot
	// silently widen this to the whole usage text and satisfy the size check below from
	// somebody else's example.
	line := usage[i:]
	if j := strings.Index(line, "\n  -"); j > 0 {
		line = line[:j]
	}
	if want := humanBytes(discover.DefaultMaxTotalBytes); !strings.Contains(line, want) {
		t.Errorf("the -max-bytes usage line does not state the default %s, so somebody sizing "+
			"a run has to trigger the truncation warning to learn it:\n%s", want, line)
	}
	// Every size in the line, so the example is checked against the default rather than
	// against a literal that has to be updated here as well.
	var largest int64
	for _, f := range strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == ';' || r == ',' || r == '(' || r == ')' || r == '\n' || r == '\t'
	}) {
		var b byteSize
		if err := b.Set(f); err == nil && int64(b) > largest {
			largest = int64(b)
		}
	}
	if largest <= discover.DefaultMaxTotalBytes {
		t.Errorf("the largest size in the -max-bytes usage line is %s, which is not above the "+
			"default %s. Raising the budget is why somebody reads this flag, and an example at "+
			"or below the default suggests a change that would shrink it:\n%s",
			humanBytes(largest), humanBytes(discover.DefaultMaxTotalBytes), line)
	}
}

// The README states the default in prose, so the constant and the sentence can disagree
// silently. This is the only test in the repo that reads its own README, and the reason is
// specific rather than general: the number is not an implementation detail a reader can
// look up, it is the thing they size a runner against, and a documented budget that is
// wrong by a factor of six sends somebody to buy memory they already have.
//
// Raising the default is what proved the need. The value lived in the constant, the flag's
// help line, this paragraph, and a parse-error hint, and three of the four had to be found
// by grep — one of them, an example below the new default, was wrong for a release before
// anybody looked. A test is cheaper than remembering.
//
// Both spellings are accepted because prose takes the space and the flag does not; what is
// asserted is the pairing of number and unit, which is the part that goes stale.
func TestTheReadmeStatesTheDefaultBudget(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	rendered := humanBytes(discover.DefaultMaxTotalBytes)
	spaced := strings.TrimRight(rendered, "KMGTiB")
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB", "B"} {
		if strings.HasSuffix(rendered, unit) {
			spaced += " " + unit
			break
		}
	}
	if !strings.Contains(string(readme), rendered) && !strings.Contains(string(readme), spaced) {
		t.Errorf("README.md states neither %q nor %q, so the documented walk budget has "+
			"drifted from discover.DefaultMaxTotalBytes. A reader sizes a CI runner against "+
			"that sentence; update it in the same change as the constant", rendered, spaced)
	}
}
