// Package assemble turns extracted facts into the graph.
//
// This is the layer design §4.4 calls "build the graph — in process", and it exists as
// its own package for the reason the extractors state in their own doc comments: they
// return Facts and deliberately do not write nodes, because resolving an import to a
// module, deciding what deserves a page, and merging duplicates are the same problem
// in every language and should be solved once. This is that one place.
//
// Three properties are load-bearing, and each one is a test in this package:
//
//   - **Deterministic.** Same facts in, byte-identical graph out. The bundle is
//     committed (§8.1), so a graph that varied by map iteration order would mean CI
//     churn on every run. Every loop here walks a sorted slice.
//   - **No invention.** An edge exists only where a file or a manifest said so, and
//     carries Confidence extracted. Nothing in this package infers, and the semantic
//     pass (§4.5) is the only thing permitted to add inferred edges later.
//   - **Gaps are reported.** An import that resolved to nothing is counted, not
//     dropped silently. A structural map that quietly omits a third of a repo's
//     dependencies is worse than one that says how much it could not resolve.
package assemble

import (
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/extract"
	"github.com/3rg0n/signpost/internal/graph"
	"github.com/3rg0n/signpost/internal/manifest"
	"github.com/3rg0n/signpost/internal/vcs"
)

// Input is everything the earlier stages produced.
type Input struct {
	Discovered *discover.Result
	Source     *extract.RunResult
	Manifests  *manifest.RunResult
	// History is what git said, or nil when there was no history to read. Optional by
	// design: §4.4's deterministic core is complete without it, so every consumer of this
	// field guards on nil rather than the caller having to synthesise an empty value.
	History *vcs.Signals
}

// Result is the assembled graph plus what assembly could not account for.
type Result struct {
	Graph *graph.Graph
	// Unresolved counts import specifiers that matched neither a directory in the
	// repository nor a declared dependency, keyed by specifier. A standard-library
	// import is not counted: it is resolved, to nothing worth a node.
	Unresolved map[string]int
	// Unlinked counts import specifiers this repository owns and that point at no
	// node, keyed by specifier.
	//
	// Separate from Unresolved because the two are different facts with different
	// fixes. An unresolved specifier is a name signpost could not place at all; an
	// unlinked one it placed exactly — inside a Go module, under a workspace package,
	// down a relative path — and found nothing there to draw an edge to. The resolver
	// is right to invent nothing for it: an external node would misreport the supply
	// chain. But it was also reporting nothing, so a module whose imports all landed
	// here appeared to import nothing at all, with the coverage report agreeing.
	//
	// A first-party import with no target is normal in small numbers — generated code,
	// a build-tagged directory, a package whose files all exceed the size cap. In large
	// numbers it means a resolution root is missing, which is what the tsconfig `paths`
	// gap looked like from the outside: 542 edges absent, nothing saying so.
	Unlinked map[string]int
	// DroppedEdges is the number of edges removed for pointing at a node that does
	// not exist. Non-zero is a bug in this package, and the CLI reports it.
	DroppedEdges int
}

// Build assembles the graph.
//
// Order is fixed and matters: every node is created before any edge is drawn, because
// an import points wherever it likes and a resolver that could only see nodes created
// so far would resolve differently depending on path order. That would make the graph
// depend on traversal, which is exactly what §8.1 forbids.
func Build(in Input) (*Result, error) {
	b := &builder{
		g:          graph.New(),
		ids:        newIDs(),
		res:        newResolver(),
		unresolved: make(map[string]int),
		unlinked:   make(map[string]int),
		in:         in,
	}
	if err := b.run(); err != nil {
		return nil, err
	}
	return &Result{
		Graph: b.g, Unresolved: b.unresolved, Unlinked: b.unlinked,
		DroppedEdges: b.dropped,
	}, nil
}

type builder struct {
	g          *graph.Graph
	ids        *ids
	res        *resolver
	in         Input
	unresolved map[string]int
	unlinked   map[string]int
	dropped    int

	// moduleFiles groups source facts by the module directory they belong to, so a
	// module node's description can be written from all of its files at once.
	moduleFiles map[string][]extract.Facts
	// testFiles marks paths discover classified as tests. The flag lives on
	// discover.File and not on extract.Facts, so it is carried across here rather
	// than re-derived from the filename — one place decides what a test is.
	testFiles map[string]bool
	// hasProdSource marks directories holding at least one non-test source file, so
	// addTestEdges can tell a test sitting beside its subject from one in a directory
	// of its own.
	hasProdSource map[string]bool
}

func (b *builder) run() error {
	b.index()
	// Every short name counted before any is assigned, so that a name two things want is
	// suffixed for both of them and neither depends on which was seen first. It has to be
	// one pass over all of them rather than one per pass below, because /references/ has
	// two sources — an external dependency and an ADR both land there — and a collision
	// across the two is no different from one within either.
	b.reserveIDs()
	if err := b.addModules(); err != nil {
		return err
	}
	if err := b.addExternals(); err != nil {
		return err
	}
	if err := b.addServices(); err != nil {
		return err
	}
	if err := b.addInterfaces(); err != nil {
		return err
	}
	if err := b.addData(); err != nil {
		return err
	}
	if err := b.addDocuments(); err != nil {
		return err
	}
	// Edges last, for the reason Build's comment gives.
	b.addImportEdges()
	b.addDeclaredDepEdges()
	b.addTestEdges()
	b.addServiceEdges()
	b.addDocEdges()
	b.addOwnerEdges()
	// History last of all, and only onto nodes the structural pass already created: it
	// annotates the map rather than deciding what is on it. A directory with history but
	// no source is not a module — deleted code still has history, and a node for it would
	// be a page about something that is not there.
	b.addHistory()
	b.addCoChangeEdges()
	b.dropped = b.g.DropDangling()
	return nil
}

// reserveIDs counts every short name that will be assigned, before any of them is.
//
// The lists here have to match what the add* passes actually name, entry for entry, which
// is why the two that filter — externals, and the document kinds — are read through the
// same helper or the same switch rather than re-derived. A name counted for something that
// never gets a page suffixes the page that does get one, for a collision that does not
// exist; a name missed leaves that entry order-dependent again.
//
// Only names matter, not keys: reserve is asking which short names more than one thing
// wants.
func (b *builder) reserveIDs() {
	dirs := make([]string, 0, len(b.moduleFiles))
	for d := range b.moduleFiles {
		dirs = append(dirs, moduleName(d))
	}
	b.ids.reserve(prefixModule, dirs)

	if b.in.Manifests == nil {
		return
	}
	order, byKey := b.externals()
	refs := make([]string, 0, len(order))
	for _, key := range order {
		e := byKey[key]
		refs = append(refs, e.eco+"-"+e.name)
	}
	// Services and data stores are folded by name before they are assigned — one node per
	// service across every compose file that defines it — so the same name twice is one
	// entry and not a collision. Deduplicated here for that reason: counting occurrences
	// would suffix the page of every service named in two files.
	services, data := map[string]bool{}, map[string]bool{}
	var interfaces []string
	for _, f := range b.in.Manifests.Facts {
		for _, s := range f.Services {
			if s.Name == "" || s.Kind == "helm-values" {
				continue
			}
			services[s.Name] = true
		}
		for _, m := range f.Migrations {
			for _, t := range m.Tables {
				data[t] = true
			}
		}
		switch f.Kind {
		case manifest.KindProto, manifest.KindOpenAPI, manifest.KindGraphQL:
			if len(f.Contracts) > 0 {
				interfaces = append(interfaces, contractName(f))
			}
		case manifest.KindADR, manifest.KindAgentRules:
			if len(f.Rules) > 0 {
				// The same prefix as the externals above, and reserve accumulates, so a
				// document and a dependency wanting one name is counted as the collision
				// it is.
				refs = append(refs, documentName(f))
			}
		}
	}
	b.ids.reserve(prefixReference, refs)
	b.ids.reserve(prefixService, sortedKeys(services))
	b.ids.reserve(prefixInterface, interfaces)
	b.ids.reserve(prefixData, sortedKeys(data))
}

// index records what exists, before anything is named.
func (b *builder) index() {
	b.moduleFiles = make(map[string][]extract.Facts)
	b.testFiles = make(map[string]bool)
	b.hasProdSource = make(map[string]bool)

	if b.in.Discovered != nil {
		for _, f := range b.in.Discovered.Files {
			if f.IsTest {
				b.testFiles[f.Path] = true
			}
		}
	}
	if b.in.Source != nil {
		for _, f := range b.in.Source.Facts {
			b.res.srcFiles[f.Path] = true
			// A test file belongs to the module it tests, not to a module of its own:
			// a `_test` node per package would double the graph and say nothing.
			dir := dirOf(f.Path)
			b.moduleFiles[dir] = append(b.moduleFiles[dir], f)
			if !b.testFiles[f.Path] {
				b.hasProdSource[dir] = true
			}
			// A JVM package is declared in the source, not in a manifest — the only
			// language here whose resolution map is built from extracted facts. Tests are
			// registered too, and marked: the standard layout declares each package twice,
			// once per source set, so an import of `com.example.api` names both
			// `src/main/java/com/example/api` and `src/test/java/com/example/api` and only
			// the first is what another module means by it.
			if f.Lang == discover.LangJava || f.Lang == discover.LangKotlin {
				b.res.addJVMPackage(f.Path, f.Package, b.testFiles[f.Path])
			}
		}
	}
	if b.in.Manifests != nil {
		b.res.ecosystems = manifestEcosystems(b.in.Manifests.Facts)
		for _, f := range b.in.Manifests.Facts {
			switch f.Kind {
			case manifest.KindGoMod:
				b.res.addGoModule(f.Path, f.Module.Name)
			case manifest.KindPackageJSON:
				b.res.addNpmPackage(f.Path, f.Module.Name)
			case manifest.KindCargo:
				b.res.addCrate(f.Path)
			case manifest.KindPyProject:
				b.res.addPyRoot(dirOf(f.Path))
			case manifest.KindTSConfig:
				b.res.addTSConfig(f.Path, f.Resolution)
			}
		}
		// After the loop, because a tsconfig may extend one that appears later in path
		// order — a package config extending the root is the common case — so the
		// inheritance can only be resolved once every config is registered.
		b.res.flattenTSAliases()
	}
}

// isTestFacts reports whether facts came from a test file. The discovery result is
// the authority — it is where IsTest was decided — and extract.Facts does not carry
// the flag.
func (b *builder) isTest(p string) bool { return b.testFiles[p] }

// addModules creates one node per directory holding extracted source.
//
// A directory rather than a language module, and the reason is that the four
// first-class languages disagree about what a module is — a Go package is a
// directory, a Rust module is a file or a directory, a Python package is a directory
// with a marker, TypeScript has none — while every one of them agrees that files in a
// directory belong together. Directory granularity is the one grouping that is
// correct in all four and needs no per-language special case.
func (b *builder) addModules() error {
	dirs := make([]string, 0, len(b.moduleFiles))
	for d := range b.moduleFiles {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		facts := b.moduleFiles[dir]
		id := b.ids.assign(prefixModule, dir, moduleName(dir))
		b.res.moduleIDs[dir] = id

		n := &graph.Node{
			ID:    id,
			Kind:  graph.KindModule,
			Title: moduleTitle(dir),
			Path:  dir,
			Attrs: map[string]string{},
		}
		var files, entrypoints, langs []string
		var incomplete int
		pkg := ""
		exported := 0
		for _, f := range facts {
			files = append(files, f.Path)
			if f.Lang != "" {
				langs = append(langs, string(f.Lang))
			}
			if pkg == "" {
				pkg = f.Package
			}
			entrypoints = append(entrypoints, f.Entrypoints...)
			if f.Incomplete {
				incomplete++
			}
			for _, s := range f.Symbols {
				if s.Exported {
					exported++
				}
			}
		}
		n.Files = sortedUnique(files)
		n.Lang = dominant(langs)
		if pkg != "" {
			n.Attrs["package"] = pkg
		}
		if eps := sortedUnique(entrypoints); len(eps) > 0 {
			n.Attrs["entrypoints"] = strings.Join(eps, ", ")
			// An entrypoint is what makes a directory a place execution starts, which
			// is the single most useful thing to know about it when picking where to
			// look first.
			n.Tags = append(n.Tags, "entrypoint")
		}
		n.Attrs["files"] = strconv.Itoa(len(n.Files))
		n.Attrs["exported"] = strconv.Itoa(exported)
		if incomplete > 0 {
			// Surfaced as a tag rather than only a count: a reader scanning the index
			// should see that a module was read partially without opening its page.
			n.Tags = append(n.Tags, "partial-extraction")
			n.Attrs["incomplete_files"] = strconv.Itoa(incomplete)
		}
		n.Description = moduleDescription(n, exported)
		if err := b.g.AddNode(n); err != nil {
			return err
		}
	}
	return nil
}

// ext is one external dependency, folded across every manifest that declares it.
type ext struct {
	eco, name string
	// local is the repo-relative directory a declaration named, when the reader
	// resolved one. Set from Dep.Local, which is the only authority: a Terraform module
	// source of `modules/rds` and one of `hashicorp/vpc/aws` are the same shape.
	local     string
	versions  []string
	scopes    []string
	sources   []string
	manifests []string
}

// externals folds the declared dependencies into the entries that will become pages, in
// assignment order.
//
// Declared, never imported: see the resolver's doc comment. Keyed by ecosystem and name so
// `serde` the crate and `serde` an npm package are two entries, because they are two things
// with two separate advisory streams.
//
// **A monorepo's own packages are excluded, and this is a correctness rule rather than
// a tidiness one.** A workspace declares its sibling packages as ordinary dependencies
// — `"@scope/core": "workspace:*"` — so reading the manifest literally turns
// first-party source into a third-party dependency page. That is a false claim about
// the supply chain in the direction that misleads: a reader auditing what this
// repository pulls in from outside gets entries nobody publishes to them, and cannot
// tell which of the two kinds each one is. Measured on a real monorepo before this
// exclusion: 60 of 81 scoped "external dependencies" were directories in the tree.
// The declaration is not discarded — addDeclaredDepEdges draws it onto the module that
// holds the package's source instead, which is what it was always describing.
//
// Its own function rather than part of addExternals because reserveIDs needs exactly this
// list — the same grouping and the same exclusion — before any ID is assigned. Recomputing
// it there would be two places that have to agree about which declarations become pages,
// and a disagreement suffixes a page for a collision that does not exist.
func (b *builder) externals() ([]string, map[string]*ext) {
	byKey := make(map[string]*ext)
	var order []string
	for _, f := range b.in.Manifests.Facts {
		for _, d := range f.Deps {
			if d.Name == "" {
				continue
			}
			key := d.Ecosystem + "\x00" + d.Name
			e, ok := byKey[key]
			if !ok {
				e = &ext{eco: d.Ecosystem, name: d.Name}
				byKey[key] = e
				order = append(order, key)
			}
			if d.Local && e.local == "" {
				e.local = d.Source
			}
			e.versions = append(e.versions, d.Version)
			e.scopes = append(e.scopes, string(d.Scope))
			e.sources = append(e.sources, d.Source)
			e.manifests = append(e.manifests, f.Path)
		}
	}
	sort.Strings(order)

	// A package this repository contains is not an external dependency, whatever the
	// manifest calls it. Dropped here rather than skipped mid-assignment so that both
	// callers see the same set: no page is written for it and nothing points at one, and
	// the declaration still becomes an edge onto the package's own module in
	// addDeclaredDepEdges.
	kept := order[:0]
	for _, key := range order {
		e := byKey[key]
		if e.eco == "npm" {
			if _, inRepo := b.res.npmSibling(e.name); inRepo {
				continue
			}
		}
		// The same rule for a declaration that named a directory in this repository
		// outright. A Terraform `module "rds" { source = "../modules/rds" }` is the
		// npm workspace sibling in another ecosystem: the manifest declares a
		// dependency and the thing depended on is code in this tree, so a reference
		// page for it says the repository imports its own infrastructure from
		// somewhere else. Dropped whether or not a module node exists at that
		// directory — a directory holding only `.tf` files has no extracted source
		// and so no node, and an external page is still the wrong answer for it.
		if e.local != "" {
			continue
		}
		kept = append(kept, key)
	}
	return kept, byKey
}

// addExternals creates one node per declared dependency. Which declarations become nodes,
// and why some do not, is externals above.
func (b *builder) addExternals() error {
	if b.in.Manifests == nil {
		return nil
	}
	order, byKey := b.externals()

	for _, key := range order {
		e := byKey[key]
		// The ecosystem is part of the ID, not just the key: two same-named packages
		// in different registries must not collide into one page.
		id := b.ids.assign(prefixReference, key, e.eco+"-"+e.name)
		b.res.addDep(e.eco, e.name, id)

		n := &graph.Node{
			ID:    id,
			Kind:  graph.KindExternal,
			Title: e.name,
			Tags:  []string{"external", e.eco},
			Attrs: map[string]string{"ecosystem": e.eco, "name": e.name},
		}
		if vs := sortedUnique(e.versions); len(vs) > 0 {
			n.Attrs["version"] = strings.Join(vs, ", ")
		}
		scopes := sortedUnique(e.scopes)
		n.Attrs["scope"] = strings.Join(scopes, ", ")
		if !containsStr(scopes, string(manifest.ScopeIndirect)) || len(scopes) > 1 {
			// Direct is the distinction §2 turns on: a direct dependency is one this
			// repository can bump itself.
			n.Tags = append(n.Tags, "direct")
		}
		if srcs := sortedUnique(e.sources); len(srcs) > 0 {
			// A git or path dependency has no registry to publish an advisory
			// against, which is a different supply-chain posture entirely.
			n.Attrs["source"] = strings.Join(srcs, ", ")
			n.Tags = append(n.Tags, "unregistered-source")
		}
		n.Files = sortedUnique(e.manifests)
		n.Description = e.eco + " dependency " + e.name
		if v := n.Attrs["version"]; v != "" {
			n.Description += " (" + v + ")"
		}
		if err := b.g.AddNode(n); err != nil {
			return err
		}
	}
	return nil
}

// addServices creates one node per runnable unit.
//
// Services from different files with the same name are one service: a compose file
// and a Kubernetes manifest describing `api` describe the same thing, and the union
// of what both say is the whole picture. That merge is why Service nodes are keyed by
// name alone.
func (b *builder) addServices() error {
	if b.in.Manifests == nil {
		return nil
	}
	type svcAgg struct {
		name  string
		items []struct {
			svc  manifest.Service
			file string
		}
	}
	byName := make(map[string]*svcAgg)
	var order []string
	for _, f := range b.in.Manifests.Facts {
		for _, s := range f.Services {
			if s.Name == "" {
				continue
			}
			// A helm-values pseudo-service records the keys a chart configures, not a
			// workload. It is configuration, and modelling it as a runnable unit
			// would put something in the services index that cannot be run.
			if s.Kind == "helm-values" {
				continue
			}
			a, ok := byName[s.Name]
			if !ok {
				a = &svcAgg{name: s.Name}
				byName[s.Name] = a
				order = append(order, s.Name)
			}
			a.items = append(a.items, struct {
				svc  manifest.Service
				file string
			}{s, f.Path})
		}
	}
	sort.Strings(order)

	for _, name := range order {
		a := byName[name]
		id := b.ids.assign(prefixService, name, name)
		n := &graph.Node{
			ID:    id,
			Kind:  graph.KindService,
			Title: name,
			Attrs: map[string]string{},
		}
		var images, ports, env, vols, kinds, ns, files, secrets []string
		for _, it := range a.items {
			files = append(files, it.file)
			if it.svc.Image != "" {
				images = append(images, it.svc.Image)
			}
			ports = append(ports, it.svc.Ports...)
			env = append(env, it.svc.EnvKeys...)
			vols = append(vols, it.svc.Volumes...)
			if it.svc.Kind != "" {
				kinds = append(kinds, it.svc.Kind)
			}
			if it.svc.Namespace != "" {
				ns = append(ns, it.svc.Namespace)
			}
			if it.svc.Build != "" {
				n.Attrs["build"] = it.svc.Build
			}
			if it.svc.Replicas != "" {
				n.Attrs["replicas"] = it.svc.Replicas
			}
		}
		// Secret references attach to this service by name, falling back to the file's
		// unattributed references — see SecretNamesFor. Names, never values.
		//
		// Per service rather than per file, which it used to be: a compose file declares
		// many services, and giving each of them every secret named anywhere in the file
		// reported a reverse proxy as reading the database password. An over-broad claim
		// here is worse than a missing one, because "this service reads that credential"
		// is exactly the kind of fact a reader acts on without re-deriving it.
		fileSet := sortedUnique(files)
		for _, f := range b.in.Manifests.Facts {
			if !containsStr(fileSet, f.Path) {
				continue
			}
			secrets = append(secrets, f.SecretNamesFor(name)...)
		}
		n.Files = fileSet
		setJoined(n.Attrs, "image", images)
		setJoined(n.Attrs, "ports", ports)
		setJoined(n.Attrs, "env", env)
		setJoined(n.Attrs, "volumes", vols)
		setJoined(n.Attrs, "workload", kinds)
		setJoined(n.Attrs, "namespace", ns)
		if s := sortedUnique(secrets); len(s) > 0 {
			n.Attrs["secret_refs"] = strings.Join(s, ", ")
			n.Tags = append(n.Tags, "reads-secrets")
		}
		if containsStr(kinds, "Secret") {
			n.Tags = append(n.Tags, "secret")
		}
		n.Description = serviceDescription(n, kinds)
		if err := b.g.AddNode(n); err != nil {
			return err
		}
	}
	return nil
}

// addInterfaces creates one node per contract file.
//
// Per file rather than per declaration: a `.proto` with twelve messages is one API
// surface a reader consults as a whole, and twelve pages would bury the service
// definition that matters among its parameter types. The declarations become
// attributes on the one node.
func (b *builder) addInterfaces() error {
	if b.in.Manifests == nil {
		return nil
	}
	for _, f := range b.in.Manifests.Facts {
		if len(f.Contracts) == 0 {
			continue
		}
		switch f.Kind {
		case manifest.KindProto, manifest.KindOpenAPI, manifest.KindGraphQL:
		default:
			// A CRD found in a Kubernetes manifest is recorded as a contract by the
			// reader; it belongs to the service page, not to its own interface page.
			continue
		}
		id := b.ids.assign(prefixInterface, f.Path, contractName(f))
		n := &graph.Node{
			ID:    id,
			Kind:  graph.KindInterface,
			Title: contractName(f),
			Path:  f.Path,
			Files: []string{f.Path},
			Tags:  []string{string(f.Kind)},
			Attrs: map[string]string{"format": string(f.Kind)},
		}
		byKind := make(map[string][]string)
		for _, c := range f.Contracts {
			byKind[c.Kind] = append(byKind[c.Kind], c.Name)
		}
		for _, k := range sortedKeys(byKind) {
			n.Attrs[k] = strings.Join(sortedUnique(byKind[k]), ", ")
		}
		if f.Module.Name != "" {
			n.Attrs["package"] = f.Module.Name
		}
		if f.Incomplete {
			n.Tags = append(n.Tags, "partial-extraction")
		}
		n.Description = interfaceDescription(f, byKind)
		if err := b.g.AddNode(n); err != nil {
			return err
		}
	}
	return nil
}

// addData creates one data-store node per table a migration touches.
//
// The table, not the migration file, is the durable concept: a migration is an event
// and a table is a thing that exists afterwards. Grouping by table is also what makes
// the history readable — every change to `things`, in order, on one page.
func (b *builder) addData() error {
	if b.in.Manifests == nil {
		return nil
	}
	type tbl struct {
		migrations  []string
		destructive bool
		files       []string
	}
	byTable := make(map[string]*tbl)
	for _, f := range b.in.Manifests.Facts {
		for _, m := range f.Migrations {
			for _, t := range m.Tables {
				a, ok := byTable[t]
				if !ok {
					a = &tbl{}
					byTable[t] = a
				}
				label := m.Version
				if m.Name != "" {
					label = strings.TrimSpace(m.Version + " " + m.Name)
				}
				a.migrations = append(a.migrations, label)
				a.destructive = a.destructive || m.Destructive
				a.files = append(a.files, f.Path)
			}
		}
	}
	for _, name := range sortedKeys(byTable) {
		a := byTable[name]
		id := b.ids.assign(prefixData, name, name)
		n := &graph.Node{
			ID:    id,
			Kind:  graph.KindDataStore,
			Title: name,
			Files: sortedUnique(a.files),
			Attrs: map[string]string{
				"table":      name,
				"migrations": strconv.Itoa(len(a.migrations)),
			},
			Tags: []string{"table"},
		}
		// Migration order is the schema's history and is preserved: the caller
		// established it by sorted path, and re-sorting would misreport the sequence.
		n.Attrs["history"] = strings.Join(a.migrations, "; ")
		n.Description = "Table " + name + ", changed by " + plural(len(a.migrations), "migration")
		if a.destructive {
			// The class of change an agent should never make casually, and the flag a
			// reader most needs before touching this table.
			n.Tags = append(n.Tags, "destructive-history")
			n.Description += "; at least one dropped a table or column"
		}
		if err := b.g.AddNode(n); err != nil {
			return err
		}
	}
	return nil
}

// addDocuments creates a node per human-authored input that states a constraint.
//
// ADRs and agent-rules files only. An ordinary README is a document, but it makes no
// binding claim, and a document node per markdown file would swamp the index with
// prose nobody is bound by. A stated constraint is different in kind: it is the thing
// an agent must not violate, and §4.5 names it as the highest-value non-code input.
func (b *builder) addDocuments() error {
	if b.in.Manifests == nil {
		return nil
	}
	for _, f := range b.in.Manifests.Facts {
		switch f.Kind {
		case manifest.KindADR, manifest.KindAgentRules:
		default:
			continue
		}
		if len(f.Rules) == 0 {
			continue
		}
		id := b.ids.assign(prefixReference, f.Path, documentName(f))
		n := &graph.Node{
			ID:    id,
			Kind:  graph.KindDocument,
			Title: documentName(f),
			Path:  f.Path,
			Files: []string{f.Path},
			Tags:  []string{string(f.Kind), "constraint"},
			Attrs: map[string]string{"rules": strconv.Itoa(len(f.Rules))},
		}
		if f.Kind == manifest.KindADR {
			// The status is what decides whether a decision still binds. A superseded
			// ADR read as current is worse than an unread one.
			if st := adrStatus(f); st != "" {
				n.Attrs["status"] = st
				n.Tags = append(n.Tags, strings.ToLower(st))
			}
			if f.Module.Version != "" {
				n.Attrs["number"] = f.Module.Version
			}
		}
		var headings []string
		for _, r := range f.Rules {
			if r.Heading != "" {
				headings = append(headings, r.Heading)
			}
		}
		setJoined(n.Attrs, "sections", headings)
		n.Description = documentDescription(f)
		if err := b.g.AddNode(n); err != nil {
			return err
		}
	}
	return nil
}

// addImportEdges draws one edge per resolved import.
func (b *builder) addImportEdges() {
	if b.in.Source == nil {
		return
	}
	for _, f := range b.in.Source.Facts {
		from := b.res.moduleAt(dirOf(f.Path))
		if from == "" {
			continue
		}
		for _, im := range f.Imports {
			to, internal := b.res.resolveImport(f.Lang, f.Path, im.Raw)
			if to == "" {
				// Counted per specifier in both maps, so a repo importing one
				// unplaceable package from forty files reports one gap, not forty.
				switch {
				case internal:
					// Placed inside this repository and pointing at no node. This
					// branch used to be empty, which made it the quietest failure in
					// the pipeline: the resolver knew the import was first-party,
					// declined to invent an external node for it — correctly — and
					// then nothing recorded that an edge had gone missing. A module
					// whose every import landed here read as importing nothing.
					b.unlinked[string(f.Lang)+" "+im.Raw]++
				case !isStdlib(f.Lang, im.Raw):
					b.unresolved[string(f.Lang)+" "+im.Raw]++
				}
				continue
			}
			b.g.AddEdge(graph.Edge{
				From: from, To: to,
				Kind: graph.EdgeImports,
				Conf: graph.Extracted,
				// Weight counts importing files, which is how strongly two modules
				// are coupled — one shared helper is not the same as thirty call
				// sites, and the difference is what a reader wants to see.
				Weight: 1,
				Source: f.Path,
			})
		}
	}
}

// addDeclaredDepEdges links the module owning a manifest to what that manifest declares.
//
// Without this, a repository that declares forty dependencies and imports them from
// files signpost could not fully resolve would show forty orphan reference nodes — the
// supply chain present in the bundle but disconnected from the code, which is the one
// view §2 needs to be able to read. The manifest's directory is the scope it declares
// for, so the edge lands on the module there, or on the nearest ancestor holding one:
// a `package.json` at the repo root very often sits in a directory with no source of
// its own.
func (b *builder) addDeclaredDepEdges() {
	if b.in.Manifests == nil {
		return
	}
	for _, f := range b.in.Manifests.Facts {
		from := b.nearestModule(dirOf(f.Path))
		if from == "" {
			continue
		}
		for _, d := range f.Deps {
			to := b.ids.lookup(prefixReference, d.Ecosystem+"\x00"+d.Name)
			if to == "" && d.Ecosystem == "npm" {
				// A sibling package in this repository has no external node — see
				// addExternals — so the declaration points at the module holding its
				// source. This is the part of a monorepo's structure that is stated
				// nowhere else: which packages a package is built against.
				to, _ = b.res.npmSibling(d.Name)
			}
			if to == "" && d.Local {
				// A declaration that named a directory in this repository, for the same
				// reason and with the same outcome: no external node exists, so the edge
				// lands on the module covering that directory. Which of its own
				// directories a repository composes its infrastructure from is stated in
				// no other file.
				//
				// The nearest ancestor holding a module rather than the directory itself,
				// because a Terraform module directory holds `.tf` files and no extracted
				// source, so nothing there is a node. Landing the edge on the ancestor
				// that is one keeps the composition visible; requiring an exact match
				// would drop it for every Terraform repository, which is all of them.
				to = b.nearestModule(d.Source)
			}
			if to == "" {
				continue
			}
			// `configures` rather than `imports`: the manifest states a dependency
			// exists, which is not the same claim as a file importing it. An imports
			// edge already comes from the source, and conflating the two would lose
			// the distinction between "declared" and "actually used" — the pair that
			// finds both unused dependencies and undeclared ones.
			b.g.AddEdge(graph.Edge{
				From: from, To: to, Kind: graph.EdgeConfigures,
				Conf: graph.Extracted, Source: f.Path,
			})
		}
	}
}

// nearestModule returns the module node covering a directory, walking up until one is
// found. Used where a file's own directory holds no source.
func (b *builder) nearestModule(dir string) string {
	for {
		if id := b.res.moduleAt(dir); id != "" {
			return id
		}
		if dir == "" {
			return ""
		}
		dir = dirOf(dir)
	}
}

// addTestEdges links a module to the module holding its tests.
//
// The edge points from the code to its tests, matching the kind's name: `tested_by`.
//
// Placement decides the subject, and imports are consulted only when placement does
// not. A test file sitting beside the code tests the code it sits beside — that is
// what Go's `_test.go`, Python's `test_x.py` next to `x.py`, and a `.test.ts` beside
// its component all mean — so the edge is a self-edge and the graph drops it. The
// imports of such a file are fixture machinery, not a statement of subject:
// `assemble_test.go` imports the graph package to assert against it, which does not
// make the graph package tested by assemble. Reading imports there produces edges
// that are confidently wrong, which is worse than the absent edge, because a
// self-edge's absence loses nothing that the directory did not already say.
//
// A test in a directory of its own — a `tests/` tree, `__tests__/`, a Rust
// integration test — is the case where placement says nothing, and there the imports
// are the only available statement of what is under test.
//
// The JVM is the one language where a *third* statement exists and is stronger than
// imports. A test class declares the package it tests, and Maven and Gradle put it in a
// separate source set — `src/test/java/com/example/api` beside
// `src/main/java/com/example/api` — so the directory differs while the declaration says
// plainly which code the class is a test of. Imports cannot supply that: same-package
// access needs no import, so a JVM test of a class beside it imports everything *except*
// its subject, and reading imports alone draws the edge to every collaborator and misses
// the one thing under test.
func (b *builder) addTestEdges() {
	if b.in.Source == nil {
		return
	}
	for _, f := range b.in.Source.Facts {
		if !b.isTest(f.Path) {
			continue
		}
		dir := dirOf(f.Path)
		if b.hasProdSource[dir] {
			continue
		}
		testMod := b.res.moduleAt(dir)
		if testMod == "" {
			continue
		}
		// The declaration first, and instead of the imports rather than alongside them.
		// A JVM test's collaborators are ordinary imports and its subject is the package
		// it declares, so consulting both would report `store` as tested by a test of
		// `api` — the confidently-wrong edge this function's rule exists to avoid.
		if f.Lang == discover.LangJava || f.Lang == discover.LangKotlin {
			if to, internal := b.res.resolveImport(f.Lang, f.Path, f.Package); internal && to != "" {
				b.g.AddEdge(graph.Edge{
					From: to, To: testMod,
					Kind: graph.EdgeTestedBy, Conf: graph.Extracted,
					Weight: 1, Source: f.Path,
				})
				continue
			}
		}
		for _, im := range f.Imports {
			to, internal := b.res.resolveImport(f.Lang, f.Path, im.Raw)
			if to == "" || !internal {
				continue
			}
			b.g.AddEdge(graph.Edge{
				From: to, To: testMod,
				Kind: graph.EdgeTestedBy, Conf: graph.Extracted,
				Weight: 1, Source: f.Path,
			})
		}
	}
}

// addServiceEdges draws the edges only deployment files can supply.
func (b *builder) addServiceEdges() {
	if b.in.Manifests == nil {
		return
	}
	for _, f := range b.in.Manifests.Facts {
		for _, s := range f.Services {
			from := b.ids.lookup(prefixService, s.Name)
			if from == "" {
				continue
			}
			// depends_on is startup ordering and runtime coupling stated by a human.
			// No source-level import can supply it, which is what makes it valuable.
			for _, dep := range s.DependsOn {
				if to := b.ids.lookup(prefixService, dep); to != "" {
					b.g.AddEdge(graph.Edge{
						From: from, To: to, Kind: graph.EdgeDeploys,
						Conf: graph.Extracted, Source: f.Path,
					})
				}
			}
			// A build context names the directory whose code the service runs, which
			// is the one link from a deployment back to the source that implements it.
			if s.Build != "" {
				if to := b.res.moduleAt(buildContextDir(f.Path, s.Build)); to != "" {
					b.g.AddEdge(graph.Edge{
						From: from, To: to, Kind: graph.EdgeDeploys,
						Conf: graph.Extracted, Source: f.Path,
					})
				}
			}
		}
		// A contract file in a repository with exactly one service is that service's
		// interface. With several, which service implements it is not stated anywhere
		// this package can read, and guessing would be inference wearing extracted
		// confidence — so the edge is drawn only where it is unambiguous.
		if id := b.ids.lookup(prefixInterface, f.Path); id != "" {
			if svcs := b.g.NodesOfKind(graph.KindService); len(svcs) == 1 {
				b.g.AddEdge(graph.Edge{
					From: svcs[0].ID, To: id, Kind: graph.EdgeImplements,
					Conf: graph.Extracted, Source: f.Path,
				})
			}
		}
	}
}

// addDocEdges links an agent-rules file to the modules it governs.
//
// Placement is the scope: an `AGENTS.md` applies to the directory it sits in and
// everything beneath it, which is how every tool that reads one treats it. That makes
// the edge extracted rather than inferred — the file's location is the statement.
//
// ADRs get no such edge. A decision record's subject is stated in its prose, not in
// its path, and `docs/adr/0007-tokens-are-opaque.md` sits nowhere near the code it
// binds. Linking it to a module would require reading what it says, which is the
// semantic pass's job (§4.5) and is exactly the doc-to-code linking it names as its
// highest-value output.
func (b *builder) addDocEdges() {
	if b.in.Manifests == nil {
		return
	}
	for _, f := range b.in.Manifests.Facts {
		if f.Kind != manifest.KindAgentRules {
			continue
		}
		from := b.ids.lookup(prefixReference, f.Path)
		if from == "" {
			continue
		}
		scope := dirOf(f.Path)
		for _, n := range b.g.NodesOfKind(graph.KindModule) {
			if scope != "" && n.Path != scope && !strings.HasPrefix(n.Path, scope+"/") {
				continue
			}
			b.g.AddEdge(graph.Edge{
				From: from, To: n.ID, Kind: graph.EdgeDocuments,
				Conf: graph.Extracted, Source: f.Path,
			})
		}
	}
}

// addOwnerEdges draws ownership from CODEOWNERS onto the modules a pattern covers.
//
// Only patterns that name a directory prefix are used. A glob like `*.go` covers the
// whole repository and would attach every module to one owner, which says nothing;
// full gitignore-style matching lives in internal/discover and is not worth
// duplicating here for a signal this coarse.
func (b *builder) addOwnerEdges() {
	if b.in.Manifests == nil {
		return
	}
	for _, f := range b.in.Manifests.Facts {
		for _, o := range f.Owners {
			dir, ok := ownerDir(o.Pattern)
			if !ok {
				continue
			}
			for _, n := range b.g.NodesOfKind(graph.KindModule) {
				if n.Path != dir && !strings.HasPrefix(n.Path, dir+"/") {
					continue
				}
				// Recorded as an attribute rather than an owner node: a team is not a
				// concept in the bundle, and a page per GitHub team would be a page
				// with nothing on it.
				if cur := n.Attrs["owners"]; cur == "" {
					n.Attrs["owners"] = strings.Join(o.Owners, ", ")
				} else {
					n.Attrs["owners"] = strings.Join(sortedUnique(append(strings.Split(cur, ", "), o.Owners...)), ", ")
				}
			}
		}
	}
}

// addHistory annotates existing nodes with what git said about their directory.
//
// Attributes rather than nodes or edges, for the same reason CODEOWNERS ownership is an
// attribute: churn is a property of a module, not a concept deserving a page of its own.
//
// Only module nodes are annotated. An external dependency has no directory in this
// repository, and a service or interface node is named by a manifest whose own churn says
// nothing about the code that implements it.
func (b *builder) addHistory() {
	h := b.in.History
	if h == nil || !h.Available {
		return
	}
	for _, n := range b.g.NodesOfKind(graph.KindModule) {
		d := h.Dirs[n.Path]
		if d == nil {
			continue
		}
		n.Attrs["commits"] = strconv.Itoa(d.Commits)
		// Insertions and deletions kept separate rather than netted: +900/-880 and +20/-0
		// net the same and describe entirely different modules.
		n.Attrs["lines_added"] = strconv.Itoa(d.Insertions)
		n.Attrs["lines_removed"] = strconv.Itoa(d.Deletions)
		if d.First != "" {
			n.Attrs["first_commit"] = d.First
		}
		if d.Last != "" {
			n.Attrs["last_commit"] = d.Last
		}
		if name, share := d.TopAuthor(); name != "" {
			n.Attrs["top_author"] = name
			// Rounded to a whole percent. The precision beyond that is spurious — it is a
			// ratio of small integers — and it would put a long decimal in a committed file
			// that changes whenever a single commit lands.
			n.Attrs["top_author_share"] = strconv.Itoa(int(share*100+0.5)) + "%"
		}
	}
}

// addCoChangeEdges draws the coupling that imports do not show.
//
// This is §4.1's stated reason for reading history at all: two modules that always change
// together are coupled whether or not either imports the other, and no static read can see
// it. A handler and the migration it depends on, a proto and its generated client, a config
// key and the code that reads it — none are import edges, and all matter to an agent about
// to change one of them.
//
// The edge is Extracted, not Inferred, and the distinction is worth being precise about:
// what is extracted is the fact that these two directories appeared in the same commits N
// times, which is read from git and not guessed. The reason for the coupling is not
// extracted and the edge does not claim one. Weight carries N so a consumer can weigh a
// pair that changed together thrice against one that did so ninety times.
//
// Edges are drawn in both directions because co-change is symmetric and the graph is
// directed: a single direction would make the edge's meaning depend on which module's page
// a reader happened to open.
func (b *builder) addCoChangeEdges() {
	h := b.in.History
	if h == nil || !h.Available {
		return
	}
	// Directory pairs are folded onto module pairs before any edge is drawn, because two
	// distinct pairs can resolve to the same one: `internal/auth/testdata <-> internal/db`
	// and `internal/auth <-> internal/db` both become `internal/auth <-> internal/db`.
	// AddEdge would sum their weights, and since one commit can appear in both pairs the
	// sum can exceed the number of commits that actually touched both modules. The maximum
	// is taken instead: it is a true lower bound on the real count, which a sum is not.
	type modPair struct{ from, to string }
	weights := make(map[modPair]int)
	var order []modPair
	for _, p := range h.CoChange {
		// nearestModule rather than moduleAt: a commit touching `internal/auth/testdata`
		// is a commit touching `internal/auth`, and the directory holding the file need not
		// itself hold source. Where neither resolves — a docs-only or deleted directory —
		// there is no node to attach to and the pair is dropped.
		from, to := b.nearestModule(p.A), b.nearestModule(p.B)
		if from == "" || to == "" || from == to {
			continue
		}
		if from > to {
			from, to = to, from
		}
		k := modPair{from, to}
		if _, seen := weights[k]; !seen {
			order = append(order, k)
		}
		if p.Commits > weights[k] {
			weights[k] = p.Commits
		}
	}
	// h.CoChange arrives sorted, so first-seen order is deterministic; sorting again keeps
	// it so regardless of what a future caller hands over.
	sort.Slice(order, func(i, j int) bool {
		if order[i].from != order[j].from {
			return order[i].from < order[j].from
		}
		return order[i].to < order[j].to
	})

	for _, k := range order {
		// Both directions, because co-change is symmetric and the graph is directed: a
		// single direction would make the edge's meaning depend on which module's page a
		// reader happened to open.
		for _, e := range [2]graph.Edge{
			{From: k.from, To: k.to},
			{From: k.to, To: k.from},
		} {
			e.Kind = graph.EdgeCoChanges
			e.Conf = graph.Extracted
			e.Weight = weights[k]
			// No Source: the provenance is the repository's history rather than any file
			// in the tree, and naming a file here would misattribute it.
			b.g.AddEdge(e)
		}
	}
}

// ownerDir reduces a CODEOWNERS pattern to a directory prefix, reporting false for
// patterns that are not a plain path.
func ownerDir(pat string) (string, bool) {
	p := strings.TrimSpace(pat)
	if p == "" || p == "*" || strings.ContainsAny(p, "*?[]!") {
		return "", false
	}
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "", false
	}
	return p, true
}

// buildContextDir resolves a compose build context against the file declaring it.
func buildContextDir(manifestPath, ctx string) string {
	ctx = strings.TrimSpace(ctx)
	if ctx == "" {
		return ""
	}
	// A Containerfile path rather than a directory names the directory it sits in.
	if strings.Contains(strings.ToLower(path.Base(ctx)), "dockerfile") ||
		strings.Contains(strings.ToLower(path.Base(ctx)), "containerfile") {
		ctx = path.Dir(ctx)
	}
	return cleanDir(path.Join(dirOf(manifestPath), ctx))
}
