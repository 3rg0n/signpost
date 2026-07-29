package manifest

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// Interface contract extraction: protobuf, OpenAPI, and GraphQL SDL.
//
// Design §4.1 calls these "interface contracts", and the word is chosen: unlike a
// function in a source file, a contract is a promise to someone outside this repository.
// That makes it the highest-consequence thing an agent can change without noticing —
// removing a proto field or an OpenAPI response code breaks a consumer that is not in
// the working tree and will not appear in any test run here.
//
// All three are read for their surface only: services and their methods, paths and their
// operations, types and their fields. Not the full type graph — a bundle that reproduced
// every message definition would be a copy of the schema rather than a map of it, and
// the schema is already in the repository for anyone who needs that depth.

// ExtractProto reads a .proto file.
//
// Hand-written for the reason stated throughout this package: the grammar needed here is
// small — package, import, service/rpc, message/enum — and a protobuf parser is a
// dependency whose CVE remediation path we would own.
func ExtractProto(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindProto}

	pkg := ""
	// depth tracks brace nesting so a `rpc` inside a service is attributed to it and a
	// nested message is named for its parent.
	var scope []string
	// A service's methods accumulate into one contract, since "what this service
	// offers" is the fact a consumer needs and one contract per method would bury it.
	svcMethods := map[string][]string{}
	var svcOrder []string
	svcLine := map[string]int{}

	for _, ln := range scanProtoLines(f.Content) {
		text := ln.text
		switch {
		case strings.HasPrefix(text, "package "):
			pkg = strings.TrimSuffix(strings.TrimSpace(text[len("package "):]), ";")
		case strings.HasPrefix(text, "import "):
			// An import is a real dependency edge between contracts: a proto that
			// imports another's types cannot be changed independently of it.
			p := strings.Trim(strings.TrimSuffix(strings.TrimSpace(text[len("import "):]), ";"), `"`)
			p = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(p, "public"), "weak"))
			p = strings.Trim(p, `"`)
			if p != "" {
				facts.Deps = append(facts.Deps, Dep{
					Name: p, Scope: ScopeRuntime, Ecosystem: "proto", Line: ln.num,
				})
			}
		case strings.HasPrefix(text, "service "):
			name := protoDeclName(text, "service")
			if name != "" {
				if _, seen := svcMethods[name]; !seen {
					svcOrder = append(svcOrder, name)
					svcLine[name] = ln.num
					svcMethods[name] = nil
				}
				scope = append(scope, "service:"+name)
				continue
			}
		case strings.HasPrefix(text, "rpc "):
			sig := parseRPC(text)
			if sig != "" {
				if svc := currentProtoService(scope); svc != "" {
					svcMethods[svc] = append(svcMethods[svc], sig)
				}
			}
		case strings.HasPrefix(text, "message "), strings.HasPrefix(text, "enum "):
			kind := "message"
			if strings.HasPrefix(text, "enum ") {
				kind = "enum"
			}
			name := protoDeclName(text, kind)
			if name == "" {
				break
			}
			// A nested message is qualified by its parent, matching how it is
			// referenced from anywhere else.
			full := name
			if parent := currentProtoMessage(scope); parent != "" {
				full = parent + "." + name
			}
			facts.Contracts = append(facts.Contracts, Contract{
				Name: full, Kind: kind, Package: pkg, Line: ln.num,
			})
			if !strings.HasSuffix(text, "}") {
				scope = append(scope, "message:"+full)
			}
			continue
		}
		// Brace tracking runs after the declaration cases so a `{` opening a scope is
		// not counted twice.
		for i := 0; i < len(text); i++ {
			if text[i] == '}' && len(scope) > 0 {
				scope = scope[:len(scope)-1]
			}
		}
	}

	for _, name := range svcOrder {
		facts.Contracts = append(facts.Contracts, Contract{
			Name: name, Kind: "service", Package: pkg,
			Detail: strings.Join(svcMethods[name], ", "),
			Line:   svcLine[name],
		})
	}
	// The package is the identity other protos import, which makes it the module name
	// in the sense the fact model means.
	if pkg != "" {
		facts.Module = Module{Name: pkg, Ecosystem: "proto", Line: 1}
	}
	return facts
}

// protoLine is one logical proto statement.
type protoLine struct {
	text string
	num  int
}

// scanProtoLines strips comments and splits on `;` and braces.
//
// A proto declaration may share a line with others or wrap across several — an rpc with
// a long request type routinely does — so the statement, not the line, is the unit.
func scanProtoLines(src string) []protoLine {
	var out []protoLine
	var cur strings.Builder
	start := 0
	inBlockComment := false
	q := byte(0)

	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i, raw := range lines {
		num := i + 1
		for j := 0; j < len(raw); j++ {
			c := raw[j]
			switch {
			case inBlockComment:
				if c == '*' && j+1 < len(raw) && raw[j+1] == '/' {
					inBlockComment = false
					j++
				}
			case q != 0:
				if c == '\\' && j+1 < len(raw) {
					cur.WriteByte(c)
					j++
					cur.WriteByte(raw[j])
					continue
				}
				if c == q {
					q = 0
				}
				cur.WriteByte(c)
			case c == '"' || c == '\'':
				q = c
				cur.WriteByte(c)
			case c == '/' && j+1 < len(raw) && raw[j+1] == '/':
				j = len(raw)
			case c == '/' && j+1 < len(raw) && raw[j+1] == '*':
				inBlockComment = true
				j++
			case c == ';', c == '{', c == '}':
				if cur.Len() == 0 && c == '}' {
					out = append(out, protoLine{text: "}", num: num})
					continue
				}
				if c == '{' {
					// The brace is kept so the scope tracker sees it.
					cur.WriteByte(c)
				}
				if text := strings.TrimSpace(cur.String()); text != "" {
					if start == 0 {
						start = num
					}
					out = append(out, protoLine{text: text, num: start})
				}
				if c == '}' {
					out = append(out, protoLine{text: "}", num: num})
				}
				cur.Reset()
				start = 0
			default:
				if cur.Len() == 0 && (c == ' ' || c == '\t') {
					continue
				}
				if cur.Len() == 0 {
					start = num
				}
				cur.WriteByte(c)
			}
		}
		// A wrapped statement joins with a space so `rpc Foo(\n  Req)` reads as one.
		if cur.Len() > 0 && !strings.HasSuffix(cur.String(), " ") {
			cur.WriteByte(' ')
		}
	}
	if text := strings.TrimSpace(cur.String()); text != "" {
		out = append(out, protoLine{text: text, num: max(start, 1)})
	}
	return out
}

// protoDeclName reads the name out of a `service X {` or `message Y {` statement.
func protoDeclName(text, keyword string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(text, keyword))
	rest = strings.TrimSuffix(strings.TrimSpace(rest), "{")
	name := strings.TrimSpace(rest)
	if i := strings.IndexAny(name, " \t{"); i >= 0 {
		name = name[:i]
	}
	return name
}

// parseRPC renders an rpc declaration as a signature.
//
// The streaming markers are kept, because whether a method streams changes how a client
// must be written and is exactly the kind of thing a contract states.
func parseRPC(text string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(text, "rpc "))
	open := strings.IndexByte(rest, '(')
	if open < 0 {
		return ""
	}
	name := strings.TrimSpace(rest[:open])
	req, after := protoParen(rest[open:])
	sig := name + "(" + req + ")"
	// `returns (Resp)` — the response type, when the declaration got that far.
	if i := strings.Index(after, "("); i >= 0 && strings.Contains(strings.ToLower(after[:i+1]), "returns") {
		resp, _ := protoParen(after[i:])
		sig += " returns (" + resp + ")"
	}
	return sig
}

// protoParen reads a parenthesised group, returning its contents and the text after it.
func protoParen(s string) (string, string) {
	if !strings.HasPrefix(s, "(") {
		return "", s
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.Join(strings.Fields(s[1:i]), " "), s[i+1:]
			}
		}
	}
	return strings.Join(strings.Fields(s[1:]), " "), ""
}

// currentProtoService returns the innermost enclosing service name.
func currentProtoService(scope []string) string {
	for i := len(scope) - 1; i >= 0; i-- {
		if s, ok := strings.CutPrefix(scope[i], "service:"); ok {
			return s
		}
	}
	return ""
}

// currentProtoMessage returns the innermost enclosing message name.
func currentProtoMessage(scope []string) string {
	for i := len(scope) - 1; i >= 0; i-- {
		if s, ok := strings.CutPrefix(scope[i], "message:"); ok {
			return s
		}
	}
	return ""
}

// httpMethods are the OpenAPI path-item keys that describe an operation. The others —
// `parameters`, `summary`, `servers`, `$ref` — apply to the path rather than naming an
// operation, and treating one as an endpoint would invent a route.
var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// ExtractOpenAPI reads an OpenAPI or Swagger description.
//
// One contract per operation. That granularity is the point: "the API" is not a
// reviewable unit, whereas "DELETE /v1/things/{id} is public and returns 204" is, and it
// is the level at which a breaking change actually happens.
func ExtractOpenAPI(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindOpenAPI}
	root, diag := parseStructured(f.Content)
	facts.applyDiag(diag)

	version := firstNonEmpty(root.Get("openapi").String(), root.Get("swagger").String())
	if version == "" {
		// Not an OpenAPI document. A YAML or JSON file under an api/ directory may be
		// anything, and empty facts are the honest reading — the same content-decides
		// rule the Kubernetes reader follows.
		return Facts{Path: f.Path, Class: f.Class, Kind: KindOpenAPI}
	}
	facts.Module = Module{
		Name:      root.Path("info", "title").String(),
		Version:   root.Path("info", "version").String(),
		Ecosystem: "openapi",
		// The spec version decides what shapes are legal, which is the same kind of
		// fact a language version is.
		LangVersion: version,
		Line:        1,
	}

	// A server URL is where this API is actually reachable, which is the closest thing
	// a schema has to a deployment fact.
	for _, s := range root.Get("servers").Seq() {
		if u := s.Get("url").String(); u != "" {
			facts.Services = append(facts.Services, Service{
				Name: u, Kind: "openapi-server", Line: s.Line,
			})
		}
	}
	if h := root.Get("host").String(); h != "" {
		// Swagger 2.0 splits what OpenAPI 3 joins.
		facts.Services = append(facts.Services, Service{
			Name: h + root.Get("basePath").String(), Kind: "openapi-server", Line: 1,
		})
	}

	paths := root.Get("paths")
	paths.Each(func(route string, item *Node) bool {
		item.Each(func(method string, op *Node) bool {
			if !httpMethods[strings.ToLower(method)] {
				return true
			}
			// The operation id is what generated clients name their method, so a rename
			// breaks callers even when the route does not change.
			detail := op.Get("operationId").String()
			if codes := op.Get("responses").MapKeys(); len(codes) > 0 {
				detail = strings.TrimSpace(detail + " -> " + strings.Join(codes, ","))
			}
			facts.Contracts = append(facts.Contracts, Contract{
				Name:    strings.ToUpper(method) + " " + route,
				Kind:    "endpoint",
				Package: strings.Join(op.Get("tags").Strings(), ","),
				Detail:  detail,
				Line:    lineOf(op, paths.Line),
			})
			// A per-operation security requirement names a scheme this endpoint
			// enforces. An empty `security: []` is the notable case — it declares the
			// endpoint public, overriding the document default.
			if sec := op.Get("security"); sec != nil {
				readOpenAPISecurity(&facts, sec, strings.ToUpper(method)+" "+route)
			}
			return true
		})
		return true
	})

	// The security schemes are the authentication surface: which credentials this API
	// accepts, named, with no value anywhere in a schema to begin with.
	for _, loc := range [][]string{{"components", "securitySchemes"}, {"securityDefinitions"}} {
		root.Path(loc...).Each(func(name string, spec *Node) bool {
			kind := spec.Get("type").String()
			if s := spec.Get("scheme").String(); s != "" {
				kind += "/" + s
			}
			facts.SecretRefs = append(facts.SecretRefs, SecretRef{
				Name: name, Key: kind, Source: "openapi-security", Line: spec.Line,
			})
			return true
		})
	}
	// The document-level requirement applies to every operation that does not override
	// it, which is how most APIs express "authenticated by default".
	readOpenAPISecurity(&facts, root.Get("security"), "")

	// Schema names are the shared types the endpoints exchange. Names only: the field
	// graph is what the schema file itself is for.
	for _, loc := range [][]string{{"components", "schemas"}, {"definitions"}} {
		schemas := root.Path(loc...)
		schemas.Each(func(name string, spec *Node) bool {
			facts.Contracts = append(facts.Contracts, Contract{
				Name: name, Kind: "schema",
				Detail: strings.Join(spec.Get("required").Strings(), ","),
				Line:   lineOf(spec, schemas.Line),
			})
			return true
		})
	}
	if len(facts.Contracts) == 0 {
		facts.markIncomplete("no paths or schemas found")
	}
	return facts
}

// readOpenAPISecurity records the schemes a security requirement names.
func readOpenAPISecurity(facts *Facts, sec *Node, endpoint string) {
	if sec == nil {
		return
	}
	for _, req := range sec.Seq() {
		req.Each(func(scheme string, scopes *Node) bool {
			facts.SecretRefs = append(facts.SecretRefs, SecretRef{
				Name: scheme, Key: strings.Join(scopes.Strings(), ","),
				EnvVar: endpoint, Source: "openapi-security", Line: req.Line,
			})
			return true
		})
	}
}

// parseStructured reads a document that may be YAML or JSON.
//
// An OpenAPI description is routinely either, and the extension does not settle it: a
// `.yaml` file holding JSON is common enough that name-based dispatch would misread real
// schemas. Sniffing the first non-space byte does settle it. A JSON document is valid
// YAML flow syntax, so the YAML reader would cope — but the JSON reader handles escapes
// exactly and reports a position on failure, so it is preferred where the content says
// JSON.
func parseStructured(content string) (*Node, Diag) {
	// A BOM is stripped alongside the whitespace: an editor on Windows writes one, and
	// with it in place the first byte is not `{` and a JSON schema reads as YAML.
	trimmed := strings.TrimLeft(content, " \t\r\n\ufeff")
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		root, err := ParseJSON(trimmed)
		if err != nil {
			var diag Diag
			diag.note(1, "JSON did not parse: "+err.Error())
			return mapNode(1), diag
		}
		return root, Diag{}
	}
	return ParseYAMLDoc(content)
}

// ExtractGraphQL reads a GraphQL schema definition.
//
// A GraphQL schema is one contract in a way a REST API is not — there is a single graph,
// and every field on Query is an entry point into it. Types and their fields are read;
// the fields are the contract, since a client selects them by name and a removal breaks
// that client at run time with no compile step to catch it.
func ExtractGraphQL(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindGraphQL}

	var curType, curKind string
	var fields []string
	var typeLine int
	depth := 0

	flush := func() {
		if curType == "" {
			return
		}
		facts.Contracts = append(facts.Contracts, Contract{
			Name: curType, Kind: curKind, Detail: strings.Join(fields, ", "), Line: typeLine,
		})
		curType, curKind, fields, typeLine = "", "", nil, 0
	}

	for i, raw := range strings.Split(strings.ReplaceAll(f.Content, "\r\n", "\n"), "\n") {
		num := i + 1
		line := stripGraphQLComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if kind, name, ok := graphQLTypeDecl(trimmed); ok {
			flush()
			curType, curKind, typeLine = name, kind, num
			depth = strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			// The body may begin, and end, on the declaration line: `type Ping { ok:
			// Boolean! }` is ordinary SDL, and skipping the rest of the line would
			// record the type with no fields — which reads as an empty contract rather
			// than as one this reader did not finish.
			if open := strings.IndexByte(trimmed, '{'); open >= 0 {
				fields = append(fields, graphQLBodyFields(trimmed[open+1:])...)
				if depth <= 0 {
					flush()
					depth = 0
				}
				continue
			}
			// No brace means no body. A union's members and an enum's values are the
			// contract in the same sense a field is, and they sit on the declaration
			// line after `=`.
			if eq := strings.IndexByte(trimmed, '='); eq >= 0 {
				fields = append(fields, graphQLBodyFields(trimmed[eq+1:])...)
			}
			flush()
			depth = 0
			continue
		}

		depth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
		if depth <= 0 && curType != "" {
			// The closing brace of a type body. Any field on this line still counts.
			if fld := graphQLFieldName(trimmed); fld != "" {
				fields = append(fields, fld)
			}
			flush()
			depth = 0
			continue
		}
		if curType == "" {
			// A directive, a schema block, or an import comment. Nothing §4.1 asks for.
			continue
		}
		if fld := graphQLFieldName(trimmed); fld != "" {
			fields = append(fields, fld)
		}
	}
	flush()

	if len(facts.Contracts) == 0 {
		facts.markIncomplete("no type definitions found")
	}
	return facts
}

// graphQLTypeDecls are the SDL keywords that introduce a named type.
var graphQLTypeDecls = []string{
	"type", "interface", "union", "enum", "input", "scalar", "schema", "directive",
}

// graphQLTypeDecl reports whether a line opens a type definition, and which.
func graphQLTypeDecl(line string) (string, string, bool) {
	rest := line
	// `extend type X` — the fields are additive, and the kind records that.
	extend := false
	if r, ok := strings.CutPrefix(rest, "extend "); ok {
		rest, extend = strings.TrimSpace(r), true
	}
	for _, kw := range graphQLTypeDecls {
		r, ok := strings.CutPrefix(rest, kw+" ")
		if !ok {
			continue
		}
		name := strings.TrimSpace(r)
		if i := strings.IndexAny(name, " \t{(@"); i >= 0 {
			name = name[:i]
		}
		if name == "" {
			return "", "", false
		}
		kind := kw
		if extend {
			kind = "extend-" + kw
		}
		return kind, name, true
	}
	return "", "", false
}

// graphQLBodyFields reads the field names out of a body written on one line.
//
// A single-line body separates its fields with commas or nothing at all — SDL treats
// both as whitespace — and a union separates its members with `|`. Splitting on all
// three and taking the name from each part covers every one-line form without a second
// code path for each.
func graphQLBodyFields(body string) []string {
	body = strings.TrimSuffix(strings.TrimSpace(body), "}")
	var out []string
	for _, part := range strings.FieldsFunc(body, func(r rune) bool {
		return r == ',' || r == '|'
	}) {
		// A field's type may itself contain a space (`[Thing!]!` does not, but
		// `name: String = "a b"` does), so only the leading token is a name.
		if fld := graphQLFieldName(part); fld != "" {
			out = append(out, fld)
		}
	}
	return out
}

// graphQLFieldName reads a field name out of a body line.
//
// The type is deliberately dropped: a field's presence and name is what a client
// depends on, and carrying every signature would make the contract detail as long as
// the schema it summarises.
func graphQLFieldName(line string) string {
	line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "{"))
	line = strings.TrimLeft(line, "|") // A union member.
	line = strings.TrimSpace(line)
	if line == "" || line == "}" || strings.HasPrefix(line, "@") {
		return ""
	}
	name := line
	if i := strings.IndexAny(name, " \t:(@!"); i >= 0 {
		name = name[:i]
	}
	name = strings.Trim(name, `",`)
	if name == "" || name == "}" {
		return ""
	}
	// An enum value or a union member has no colon and is still the contract.
	return name
}

// stripGraphQLComment removes a `#` comment, respecting strings.
//
// A description string may contain a `#`, and a schema's descriptions are where the
// interesting `#` characters live — URLs in particular.
func stripGraphQLComment(line string) string {
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch {
		case inQuote:
			if line[i] == '\\' {
				i++
				continue
			}
			if line[i] == '"' {
				inQuote = false
			}
		case line[i] == '"':
			// A triple-quoted block description opens and closes on its own line in
			// practice; treating the run as one quote is close enough to keep a `#`
			// inside it out of the comment stripper.
			if strings.HasPrefix(line[i:], `"""`) {
				i += 2
				continue
			}
			inQuote = true
		case line[i] == '#':
			return line[:i]
		}
	}
	return line
}
