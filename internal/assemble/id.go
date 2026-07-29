package assemble

import (
	"hash/fnv"
	"strconv"
	"strings"
)

// Concept path prefixes.
//
// The bundle's directory layout (design §3) is also the node ID namespace: an ID is
// the page's path with the .md suffix removed, which is what OKF links resolve
// against. Keeping the two the same means the emitter never has to translate.
const (
	prefixModule    = "/modules/"
	prefixService   = "/services/"
	prefixInterface = "/interfaces/"
	prefixData      = "/data/"
	prefixReference = "/references/"
)

// slug reduces arbitrary text to a filesystem- and URL-safe identifier.
//
// Deliberately lossy and deliberately ASCII: the result is a filename in a committed
// bundle, checked out on Windows, macOS, and Linux, and case-insensitive filesystems
// make `Auth` and `auth` the same file. Collisions are therefore expected rather than
// avoided here, and are resolved by ids.assign, which sees every name at once.
func slug(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// ids assigns collision-free concept paths.
//
// Two distinct things can slug to the same text — `internal/auth` and `internal-auth`
// both become "internal-auth" — and the graph would silently merge them, because
// AddNode treats a repeated ID as the same node. That is a wrong graph rather than an
// ugly one, so collisions are resolved by suffixing. The suffix is positional, which
// makes it stable only if callers assign in a fixed order; every caller iterates
// already-sorted input for that reason.
type ids struct {
	byKey map[string]string
	used  map[string]bool
}

func newIDs() *ids {
	return &ids{byKey: make(map[string]string), used: make(map[string]bool)}
}

// assign returns the concept path for a key, creating it on first sight.
//
// key identifies the thing (a directory, a service name); name is the text to slug.
// The same key always gets the same path, so callers can ask repeatedly without
// tracking what they have already named.
func (x *ids) assign(prefix, key, name string) string {
	full := prefix + key
	if id, ok := x.byKey[full]; ok {
		return id
	}
	base := slug(name)
	if base == "" {
		// Nothing survived slugging — a directory named only in a non-Latin script,
		// or in punctuation. A hash keeps the ID deterministic and unique rather than
		// falling back to a counter that would depend on how many such names preceded.
		h := fnv.New32a()
		_, _ = h.Write([]byte(full))
		base = "x" + strconv.FormatUint(uint64(h.Sum32()), 36)
	}
	id := prefix + base
	for n := 2; x.used[id]; n++ {
		id = prefix + base + "-" + strconv.Itoa(n)
	}
	x.used[id] = true
	x.byKey[full] = id
	return id
}

// lookup returns an already-assigned path, or "" — for callers that must not create a
// node as a side effect of asking about one.
func (x *ids) lookup(prefix, key string) string { return x.byKey[prefix+key] }
