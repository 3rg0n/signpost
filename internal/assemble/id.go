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
	prefixPipeline  = "/pipelines/"
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
// ugly one, so collisions are resolved by suffixing.
//
// The suffix is derived from the colliding entry's own key rather than from its position
// in the run, which is what keeps a committed bundle's filenames still. A counter made
// every member of a colliding group depend on how many members preceded it: in a
// repository with `a/auth`, `b/auth`, and `c/a-u-t-h`, adding an unrelated `aa/auth`
// renamed `b/auth`'s page from auth-2 to auth-3 and deleting `a/auth` renamed it again,
// both times for a directory that had not changed. Since the ID is also the page's
// filename and the concept path every other page links against (ADR 0003), each of
// those renames rewrote unrelated pages across the bundle.
//
// Deriving from the key removes that: an entry's ID depends on its own key and on whether
// its short name is shared, and on nothing else — not on ordering, and not on how many
// other things happen to be named the same.
//
// The second half is what reserve is for. Whether a name is shared cannot be known while
// assigning one at a time, and a first-come rule is not good enough: it gives the bare
// readable name to whoever is seen first, so a new directory sorting ahead of the current
// holder takes the name off it. Adding a `src` to a repository that has two is an ordinary
// edit, and under a first-come rule roughly half of those additions rename somebody else's
// page. So the names are counted before any of them is assigned, and a shared one is
// suffixed for *every* member including the first.
type ids struct {
	byKey map[string]string
	used  map[string]bool
	// wanted holds every prefix+slug some reserved entry asked for; shared holds the ones
	// more than one asked for. An entry whose name is in shared is suffixed even if it is
	// the only one assigned so far.
	wanted map[string]bool
	shared map[string]bool
}

func newIDs() *ids {
	return &ids{
		byKey:  make(map[string]string),
		used:   make(map[string]bool),
		wanted: make(map[string]bool),
		shared: make(map[string]bool),
	}
}

// reserve records which short names under a prefix are wanted by more than one entry.
//
// Called with exactly the entries that will be assigned, before the first of them is,
// because the alternative — deciding on first sight — is what makes an ID depend on
// assignment order. Callers that skip an entry must skip it here too: a name counted for
// something that never gets a page suffixes the entry that does get one, for a collision
// that does not exist.
//
// Not required. An unreserved entry still gets a correct, unique ID; it just gets the
// order-dependent one, so a caller that forgets loses stability rather than correctness.
//
// Accumulates across calls, because a prefix can have more than one source: an external
// dependency and an ADR both become a page under /references/, and a collision between the
// two is no different from one within either.
func (x *ids) reserve(prefix string, names []string) {
	for _, name := range names {
		base := slug(name)
		if base == "" {
			// Hashed from the key rather than the name, so two of them never share a
			// short name to begin with.
			continue
		}
		id := prefix + base
		if x.wanted[id] {
			x.shared[id] = true
		}
		x.wanted[id] = true
	}
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
		base = "x" + keyHash(full)
	}
	id := prefix + base
	if x.shared[id] || x.used[id] {
		// The short name is not this entry's alone, so the entry is identified by what
		// makes it different from the others: its key. Two entries reaching here can
		// only collide with each other if their keys are equal, and equal keys are the
		// same entry, which the byKey hit above already returned.
		//
		// x.used as well as x.shared, because reserve is optional — an unreserved
		// caller still has to get a unique ID out of this, just not a stable one.
		id = prefix + base + "-" + keyHash(full)
		// Unless the two keys hash alike, which 32 bits does by the birthday bound
		// somewhere around a hundred thousand of them — reachable in a monorepo, and
		// there is a found pair in the tests. A shared ID is one page claiming to
		// describe two directories, so uniqueness wins over stability in the one case
		// where the two conflict: the counter survives here and only here.
		for n := 2; x.used[id]; n++ {
			id = prefix + base + "-" + keyHash(full) + "-" + strconv.Itoa(n)
		}
	}
	x.used[id] = true
	x.byKey[full] = id
	return id
}

// keyHash is the short stable discriminator appended to a colliding name.
//
// Truncated to nothing and encoded in base36 for the same reason slug is lossy: the
// result is a filename in a bundle that gets checked out on case-insensitive
// filesystems, so lowercase alphanumerics are the whole safe alphabet. 32 bits is
// deliberate — this is disambiguating a handful of same-named directories in one
// repository, not resisting collision attacks, and a longer hash would make every
// colliding page's filename harder to read for no gain a repository would notice.
func keyHash(key string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return strconv.FormatUint(uint64(h.Sum32()), 36)
}

// lookup returns an already-assigned path, or "" — for callers that must not create a
// node as a side effect of asking about one.
func (x *ids) lookup(prefix, key string) string { return x.byKey[prefix+key] }
