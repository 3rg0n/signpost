package manifest

import (
	"path"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// Containerfile and compose extraction.
//
// Design §4.1 asks these for "services, ports, base images, build inputs", and the
// reason they matter more than their size suggests is that they answer questions no
// source file can. An import graph says module A uses module B; a compose file says
// the api service talks to postgres on 5432, waits for it to be healthy, and reads
// its password from a secret. That is the runtime architecture, stated by a human,
// and it is nowhere in the code.

// ExtractContainerfile reads a Containerfile or Dockerfile.
//
// A Containerfile is a line-oriented instruction list, not a structured document, so
// this reads it directly rather than through the Node tree.
func ExtractContainerfile(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindContainer}

	// stages records the build stages named so far. A `FROM builder` referring to an
	// earlier stage is an internal reference, not an external base image, and
	// reporting it as one would invent a registry pull nobody makes.
	stages := map[string]bool{}
	// args holds ARG defaults so a templated FROM can be resolved. Very common:
	// `ARG BASE=cgr.dev/chainguard/static` then `FROM ${BASE}`, where an unresolved
	// FROM would record `${BASE}` as the base image — a useless fact where an exact
	// one was available.
	args := map[string]string{}
	// entrypoint and cmd are folded together at the end: what a container runs is
	// one fact, expressed across two instructions that override each other by
	// convention rather than by rule.
	var entrypoint, cmd string
	var entryLine int
	// The service's fields accumulate here rather than on Facts, since a
	// Containerfile describes exactly one unit and there is nothing to key them by.
	var envKeys, ports []string

	for _, ln := range containerLines(f.Content) {
		verb, rest := splitFirstToken(ln.text)
		verb = strings.ToUpper(verb)
		rest = strings.TrimSpace(rest)

		switch verb {
		case "FROM":
			ref, stage := parseFromInstruction(rest)
			ref = expandShellVars(ref, args)
			if stage != "" {
				stages[strings.ToLower(stage)] = true
			}
			// An earlier stage referenced as a base is internal to this build.
			if stages[strings.ToLower(ref)] && ref != "" {
				continue
			}
			facts.Images = append(facts.Images, Image{
				Ref: ref, Stage: stage, Base: true, Line: ln.num,
			})
		case "ARG", "ENV":
			for _, kv := range parseInstructionKeyValues(rest) {
				if verb == "ARG" {
					args[kv.Key] = kv.Value
				}
				// Only the name is recorded. A Containerfile ENV frequently holds a
				// token, and this bundle is committed: see the package doc's secrets
				// rule.
				envKeys = append(envKeys, kv.Key)
				if looksSensitive(kv.Key) {
					facts.SecretRefs = append(facts.SecretRefs, SecretRef{
						Name: kv.Key, EnvVar: kv.Key, Source: "containerfile-env", Line: ln.num,
					})
				}
			}
		case "EXPOSE":
			for _, p := range strings.Fields(rest) {
				ports = append(ports, expandShellVars(p, args))
			}
		case "COPY", "ADD":
			// `COPY --from=x` is a build input from another stage or an external
			// image: a real edge in the build graph, and the reason a multi-stage
			// build's final image is small.
			if src := instructionFlag(rest, "from"); src != "" && !stages[strings.ToLower(src)] {
				facts.Images = append(facts.Images, Image{Ref: expandShellVars(src, args), Line: ln.num})
			}
		case "ENTRYPOINT":
			entrypoint = joinExecForm(rest)
			entryLine = ln.num
		case "CMD":
			cmd = joinExecForm(rest)
			if entryLine == 0 {
				entryLine = ln.num
			}
		case "WORKDIR", "RUN", "LABEL", "VOLUME", "HEALTHCHECK", "SHELL",
			"USER", "STOPSIGNAL", "ONBUILD", "MAINTAINER":
			// Read and deliberately not recorded: none of these answers a question
			// §4.1 asks, and a RUN line's text is a build detail rather than
			// architecture. Not a diagnostic, because nothing was missed.
		case "":
		default:
			facts.markIncomplete("unrecognised instruction " + verb)
		}
	}

	// The service this file builds, named for its directory when the filename is the
	// conventional one — `services/api/Containerfile` builds "api". A Containerfile
	// does not name what it produces, and the directory is the only signal available.
	svc := Service{
		Name: containerServiceName(f.Path), Build: f.Path,
		EnvKeys: envKeys, Ports: ports, Line: 1,
	}
	// The last stage's base image is what this file actually produces from, so it is
	// the service's image in the sense the fact model means.
	if len(facts.Images) > 0 {
		svc.Image = facts.Images[len(facts.Images)-1].Ref
	}
	if entrypoint != "" || cmd != "" {
		// ENTRYPOINT with CMD means CMD is the default arguments; either alone is the
		// command. Joining reproduces what actually runs.
		facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
			Name: svc.Name, Path: strings.TrimSpace(entrypoint + " " + cmd), Line: entryLine,
		})
	}
	facts.Services = append(facts.Services, svc)
	return facts
}

// containerLine is one logical instruction, after continuations are joined.
type containerLine struct {
	text string
	num  int
}

// containerLines splits a Containerfile into logical instructions.
//
// A trailing backslash continues an instruction, and every real Containerfile uses
// them — a multi-line RUN with `&&` joins is the standard way to keep layer count
// down. Without joining, the continuation lines parse as instructions with
// nonsense verbs and each one becomes a spurious diagnostic.
func containerLines(src string) []containerLine {
	var out []containerLine
	var cur strings.Builder
	start := 0
	for i, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		num := i + 1
		line := raw
		// A `#` at the start of a line is a comment; one mid-line is content, since
		// a tag or a URL may contain it.
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "#") {
			// Parser directives (`# syntax=docker/dockerfile:1`) are comments to the
			// grammar and carry nothing §4.1 asks for.
			continue
		}
		continues := strings.HasSuffix(strings.TrimRight(line, " \t"), "\\")
		if continues {
			line = strings.TrimSuffix(strings.TrimRight(line, " \t"), "\\")
		}
		if cur.Len() == 0 {
			start = num
			if strings.TrimSpace(line) == "" {
				continue
			}
		}
		cur.WriteString(" ")
		cur.WriteString(strings.TrimSpace(line))
		if continues {
			continue
		}
		if text := strings.TrimSpace(cur.String()); text != "" {
			out = append(out, containerLine{text: text, num: start})
		}
		cur.Reset()
	}
	if text := strings.TrimSpace(cur.String()); text != "" {
		out = append(out, containerLine{text: text, num: start})
	}
	return out
}

// parseFromInstruction splits a FROM into its image reference and stage name.
func parseFromInstruction(rest string) (string, string) {
	fields := strings.Fields(rest)
	ref, stage := "", ""
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if strings.HasPrefix(f, "--") {
			// `--platform=linux/amd64` and friends.
			continue
		}
		if strings.EqualFold(f, "AS") && i+1 < len(fields) {
			stage = fields[i+1]
			break
		}
		if ref == "" {
			ref = f
		}
	}
	return ref, stage
}

// instructionFlag returns the value of a `--name=value` flag, or "".
func instructionFlag(rest, name string) string {
	prefix := "--" + name + "="
	for _, f := range strings.Fields(rest) {
		if strings.HasPrefix(f, prefix) {
			return strings.Trim(f[len(prefix):], `"'`)
		}
	}
	return ""
}

// parseInstructionKeyValues reads the ARG/ENV forms: `K=V K2=V2`, or the legacy
// `ENV K V` where the rest of the line is one value.
func parseInstructionKeyValues(rest string) []KeyValue {
	if !strings.Contains(rest, "=") {
		k, v := splitFirstToken(rest)
		if k == "" {
			return nil
		}
		return []KeyValue{{Key: k, Value: strings.TrimSpace(v)}}
	}
	var out []KeyValue
	for _, tok := range splitRespectingQuotes(rest) {
		k, v := splitKeyValue(tok)
		if k == "" {
			continue
		}
		out = append(out, KeyValue{Key: k, Value: strings.Trim(v, `"'`)})
	}
	return out
}

// splitRespectingQuotes splits on whitespace outside quotes.
func splitRespectingQuotes(s string) []string {
	var out []string
	var cur strings.Builder
	q := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			q = c
			cur.WriteByte(c)
		case c == ' ' || c == '\t':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// expandShellVars substitutes `$NAME` and `${NAME}` from known ARG defaults.
//
// Only known names are substituted; an unknown one is left as written, because
// `${TAG}` is more honest than an empty string where a tag belongs.
func expandShellVars(s string, vars map[string]string) string {
	if !strings.Contains(s, "$") || len(vars) == 0 {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			b.WriteByte(s[i])
			continue
		}
		j := i + 1
		braced := j < len(s) && s[j] == '{'
		if braced {
			j++
		}
		start := j
		for j < len(s) && (isVarChar(s[j])) {
			j++
		}
		name := s[start:j]
		// `${NAME:-default}` — the default is what applies when nothing is set.
		fallback := ""
		if braced && j < len(s) && s[j] == ':' {
			k := j
			for k < len(s) && s[k] != '}' {
				k++
			}
			fallback = strings.TrimLeft(s[j+1:min(k, len(s))], "-=")
			j = k
		}
		if braced {
			if j < len(s) && s[j] == '}' {
				j++
			}
		}
		val, ok := vars[name]
		switch {
		case ok && val != "":
			b.WriteString(strings.Trim(val, `"'`))
		case fallback != "":
			b.WriteString(fallback)
		default:
			b.WriteString(s[i:j])
		}
		i = j - 1
	}
	return b.String()
}

func isVarChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// joinExecForm renders an ENTRYPOINT or CMD as a command line.
//
// Both accept a JSON array (`["./app", "-v"]`) and a shell string (`./app -v`). The
// array form is not read as JSON: a Containerfile's array is frequently written with
// single quotes, which is invalid JSON that Docker nonetheless accepts, and failing
// on it would lose the command entirely.
func joinExecForm(rest string) string {
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "[") {
		return rest
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(rest, "["), "]")
	var parts []string
	for _, tok := range strings.Split(inner, ",") {
		if t := strings.Trim(strings.TrimSpace(tok), `"'`); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// containerServiceName names the unit a Containerfile builds.
//
// The file itself does not say, so the directory is used — `services/api/Dockerfile`
// builds "api" — and a variant suffix is kept, since `Dockerfile.worker` in the same
// directory builds something different.
func containerServiceName(rel string) string {
	base := path.Base(rel)
	dir := path.Dir(rel)
	name := ""
	if dir != "." && dir != "/" && dir != "" {
		name = path.Base(dir)
	}
	// A variant: Dockerfile.worker or worker.Dockerfile.
	lower := strings.ToLower(base)
	variant := ""
	switch {
	case strings.HasPrefix(lower, "dockerfile."):
		variant = base[len("dockerfile."):]
	case strings.HasPrefix(lower, "containerfile."):
		variant = base[len("containerfile."):]
	case strings.HasSuffix(lower, ".dockerfile"):
		variant = base[:len(base)-len(".dockerfile")]
	case strings.HasSuffix(lower, ".containerfile"):
		variant = base[:len(base)-len(".containerfile")]
	}
	switch {
	case name != "" && variant != "":
		return name + "-" + variant
	case variant != "":
		return variant
	case name != "":
		return name
	}
	return "image"
}

// ExtractCompose reads a compose file.
func ExtractCompose(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindCompose}
	root, diag := ParseYAMLDoc(f.Content)
	facts.applyDiag(diag)

	// The top-level `secrets:` block declares the secrets services then reference by
	// name. A `file:` entry names a path and an `environment:` entry names a
	// variable — both are references, which is why both are safe to record.
	secrets := root.Get("secrets")
	secrets.Each(func(name string, spec *Node) bool {
		facts.SecretRefs = append(facts.SecretRefs, SecretRef{
			Name:   name,
			Key:    firstNonEmpty(spec.Get("file").String(), spec.Get("environment").String()),
			Source: "compose-secret", Line: lineOf(spec, secrets.Line),
		})
		return true
	})

	services := root.Get("services")
	services.Each(func(name string, spec *Node) bool {
		svc := Service{Name: name, Line: lineOf(spec, services.Line)}
		svc.Image = spec.Get("image").String()

		// `build` is either a context string or an object.
		if b := spec.Get("build"); b != nil {
			if b.Kind == KindScalar {
				svc.Build = b.String()
			} else {
				ctx := b.Get("context").String()
				if df := b.GetAny("dockerfile", "containerfile").String(); df != "" {
					// The Containerfile path, resolved against the context, so it
					// matches the path the walker discovered and the two link up.
					svc.Build = path.Join(ctx, df)
				} else {
					svc.Build = ctx
				}
				// Build args name build-time inputs. Names only, again: a build arg
				// is a very common place to pass a registry token.
				for _, kv := range b.Get("args").KeyValues() {
					svc.EnvKeys = append(svc.EnvKeys, kv.Key)
				}
			}
		}

		svc.Ports = append(svc.Ports, composePortStrings(spec.Get("ports"))...)
		svc.Ports = append(svc.Ports, composePortStrings(spec.Get("expose"))...)
		svc.Volumes = append(svc.Volumes, composeVolumeStrings(spec.Get("volumes"))...)
		svc.DependsOn = append(svc.DependsOn, composeDependsOn(spec.Get("depends_on"))...)
		// A link or a network alias is the same runtime coupling depends_on states.
		svc.DependsOn = append(svc.DependsOn, dependencyNames(spec.Get("links"))...)

		for _, kv := range spec.Get("environment").KeyValues() {
			svc.EnvKeys = append(svc.EnvKeys, kv.Key)
			// A compose value of the form ${VAR} is a reference to the host
			// environment, and when the name looks like a credential that reference
			// is worth recording — the *name*, never the interpolated value.
			if ref := interpolatedName(kv.Value); ref != "" && looksSensitive(kv.Key) {
				facts.SecretRefs = append(facts.SecretRefs, SecretRef{
					Name: ref, EnvVar: kv.Key, Source: "env-interpolation", Line: kv.Line,
				})
			}
		}
		// env_file points at a file of variables. The path is signal; the contents
		// are exactly what must never be read into the bundle.
		for _, ef := range spec.Get("env_file").Seq() {
			p := firstNonEmpty(ef.Get("path").String(), ef.String())
			if p == "" {
				continue
			}
			facts.SecretRefs = append(facts.SecretRefs, SecretRef{
				Name: p, Source: "env-file", Line: ef.Line,
			})
		}
		// A service's declared secrets: the short form is a name, the long form an
		// object with source and target.
		for _, s := range spec.Get("secrets").Seq() {
			name := firstNonEmpty(s.Get("source").String(), s.String())
			if name == "" {
				continue
			}
			facts.SecretRefs = append(facts.SecretRefs, SecretRef{
				Name: name, Key: s.Get("target").String(),
				Source: "compose-secret", Line: s.Line,
			})
		}

		if r := spec.Path("deploy", "replicas"); !r.IsZero() {
			svc.Replicas = r.String()
		}
		if svc.Image != "" {
			facts.Images = append(facts.Images, Image{Ref: svc.Image, Line: svc.Line})
		}
		if cmd := spec.GetAny("command", "entrypoint"); !cmd.IsZero() {
			facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
				Name: name, Path: strings.Join(cmd.Strings(), " "), Line: cmd.Line,
			})
		}
		facts.Services = append(facts.Services, svc)
		return true
	})

	if services == nil {
		facts.markIncomplete("no services block found")
	}
	return facts
}

// composePortStrings reads a ports or expose entry.
//
// Three forms: "8080:80", 8080, and the long object form. All are kept as written
// rather than normalised, because "127.0.0.1:8080:80" says something
// "8080:80" does not — the port is bound to loopback only.
func composePortStrings(n *Node) []string {
	var out []string
	for _, p := range n.Seq() {
		if p.Kind == KindMap {
			pub := p.Get("published").String()
			tgt := p.Get("target").String()
			switch {
			case pub != "" && tgt != "":
				out = append(out, pub+":"+tgt)
			case tgt != "":
				out = append(out, tgt)
			}
			continue
		}
		if s := p.String(); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// composeVolumeStrings reads a volumes entry in either the short or long form.
func composeVolumeStrings(n *Node) []string {
	var out []string
	for _, v := range n.Seq() {
		if v.Kind == KindMap {
			src := firstNonEmpty(v.Get("source").String(), v.Get("type").String())
			tgt := v.Get("target").String()
			if src != "" && tgt != "" {
				out = append(out, src+":"+tgt)
			} else if tgt != "" {
				out = append(out, tgt)
			}
			continue
		}
		if s := v.String(); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// composeDependsOn reads depends_on in either the list or the condition-map form.
func composeDependsOn(n *Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == KindMap {
		return n.MapKeys()
	}
	return n.Strings()
}

// dependencyNames reads a list of `name` or `name:alias` entries.
func dependencyNames(n *Node) []string {
	var out []string
	for _, s := range n.Strings() {
		if i := strings.IndexByte(s, ':'); i >= 0 {
			s = s[:i]
		}
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// interpolatedName returns the variable name in a `${VAR}` or `$VAR` value, or "".
func interpolatedName(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "$") {
		return ""
	}
	name := strings.TrimPrefix(strings.TrimPrefix(v, "$"), "{")
	name = strings.TrimSuffix(name, "}")
	// `${VAR:-default}` — the default may itself be a literal secret, so only the
	// name is taken and everything after the operator is discarded unread.
	for _, sep := range []string{":-", ":=", ":?", "-", "?"} {
		if i := strings.Index(name, sep); i >= 0 {
			name = name[:i]
		}
	}
	if name == "" || !isVarName(name) {
		return ""
	}
	return name
}

func isVarName(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isVarChar(s[i]) {
			return false
		}
	}
	return true
}

// sensitiveWords are the substrings that mark an environment variable as carrying a
// credential.
//
// Used only to decide whether a *reference* is worth recording as a SecretRef, never
// to decide whether to read a value — no value is ever read, so a miss here loses a
// cross-reference and nothing more.
var sensitiveWords = []string{
	"password", "passwd", "secret", "token", "apikey", "api_key",
	"credential", "private_key", "privatekey", "access_key", "accesskey",
	"auth", "cert", "signing", "session_key",
}

func looksSensitive(name string) bool {
	l := strings.ToLower(name)
	for _, w := range sensitiveWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}
