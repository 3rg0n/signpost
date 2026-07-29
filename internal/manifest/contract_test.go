package manifest

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func contractFile(p, content string) discover.File {
	return discover.File{Path: p, Class: discover.ClassContract, Content: content}
}

// contractOf finds a contract by name, failing if absent.
//
// A name alone is not always unique — a GraphQL `extend type Query` and the `type Query`
// it extends share one — so an optional kind narrows it.
func contractOf(t *testing.T, f Facts, name string, kind ...string) Contract {
	t.Helper()
	for _, c := range f.Contracts {
		if c.Name != name {
			continue
		}
		if len(kind) > 0 && c.Kind != kind[0] {
			continue
		}
		return c
	}
	var got []string
	for _, c := range f.Contracts {
		got = append(got, c.Kind+" "+c.Name)
	}
	t.Fatalf("no contract named %q in %v", name, got)
	return Contract{}
}

func TestProtoExtraction(t *testing.T) {
	facts := ExtractProto(contractFile("api/v1/things.proto", `
syntax = "proto3";

package signpost.api.v1;

import "google/protobuf/timestamp.proto";
import public "common/types.proto";

option go_package = "github.com/3rg0n/signpost/api/v1;apiv1";

// ThingService manages things.
service ThingService {
  rpc GetThing(GetThingRequest) returns (Thing);
  rpc ListThings(ListThingsRequest) returns (stream Thing);
  rpc Ingest(stream IngestRequest) returns (IngestSummary);
}

message Thing {
  string id = 1;
  google.protobuf.Timestamp created_at = 2;

  message Metadata {
    map<string, string> labels = 1;
  }
}

enum Status {
  STATUS_UNKNOWN = 0;
  STATUS_READY = 1;
}
`))
	facts.Normalize()

	if facts.Module.Name != "signpost.api.v1" {
		t.Errorf("package = %q", facts.Module.Name)
	}
	depOf(t, facts, "google/protobuf/timestamp.proto", ScopeRuntime)
	// `import public` re-exports, and the qualifier is not part of the path.
	depOf(t, facts, "common/types.proto", ScopeRuntime)

	svc := contractOf(t, facts, "ThingService")
	if svc.Kind != "service" || svc.Package != "signpost.api.v1" {
		t.Errorf("service = %+v", svc)
	}
	// Whether a method streams changes how a client must be written, so the marker is
	// part of the signature rather than noise to strip.
	for _, want := range []string{
		"GetThing(GetThingRequest) returns (Thing)",
		"ListThings(ListThingsRequest) returns (stream Thing)",
		"Ingest(stream IngestRequest) returns (IngestSummary)",
	} {
		if !strings.Contains(svc.Detail, want) {
			t.Errorf("methods = %q, want %q", svc.Detail, want)
		}
	}

	if contractOf(t, facts, "Thing").Kind != "message" {
		t.Errorf("Thing = %+v", contractOf(t, facts, "Thing"))
	}
	// A nested message is qualified by its parent, matching how it is referenced from
	// anywhere else in the schema.
	contractOf(t, facts, "Thing.Metadata")
	if contractOf(t, facts, "Status").Kind != "enum" {
		t.Errorf("Status should be an enum")
	}
}

// An rpc with long types wraps across lines, and the statement rather than the line is
// the unit — a line-oriented reader would lose the return type.
func TestProtoWrappedRPC(t *testing.T) {
	facts := ExtractProto(contractFile("a.proto", `
package p;
service S {
  rpc VeryLongMethodName(
      some.package.VeryLongRequestTypeName)
      returns (
      some.package.VeryLongResponseTypeName);
}
`))
	facts.Normalize()
	svc := contractOf(t, facts, "S")
	want := "VeryLongMethodName(some.package.VeryLongRequestTypeName) returns (some.package.VeryLongResponseTypeName)"
	if svc.Detail != want {
		t.Errorf("detail = %q, want %q", svc.Detail, want)
	}
}

// A `//` inside a string is content, and a `;` inside a comment does not end a
// statement — both appear in real option lines.
func TestProtoCommentsAndStrings(t *testing.T) {
	facts := ExtractProto(contractFile("a.proto", `
package p;
/* A block comment; with a semicolon
   and a service ThingService { that is not one */
option java_package = "com.example.http://not-a-comment";
// rpc Ghost(X) returns (Y);
service Real {
  rpc Actual(X) returns (Y);
}
`))
	facts.Normalize()
	if len(facts.Contracts) != 1 {
		var names []string
		for _, c := range facts.Contracts {
			names = append(names, c.Name)
		}
		t.Fatalf("contracts = %v, want only Real", names)
	}
	svc := contractOf(t, facts, "Real")
	if svc.Detail != "Actual(X) returns (Y)" {
		t.Errorf("a commented-out rpc must not be read: %q", svc.Detail)
	}
}

func TestProtoTwoServices(t *testing.T) {
	facts := ExtractProto(contractFile("a.proto", `
package p;
service A { rpc One(X) returns (Y); }
service B { rpc Two(X) returns (Y); }
`))
	facts.Normalize()
	// Each service collects only its own methods; a leaked scope would attribute Two
	// to A and misreport both interfaces.
	if got := contractOf(t, facts, "A").Detail; got != "One(X) returns (Y)" {
		t.Errorf("A = %q", got)
	}
	if got := contractOf(t, facts, "B").Detail; got != "Two(X) returns (Y)" {
		t.Errorf("B = %q", got)
	}
}

func TestOpenAPIExtraction(t *testing.T) {
	facts := ExtractOpenAPI(contractFile("api/openapi.yaml", `
openapi: 3.1.0
info:
  title: Thing API
  version: 1.2.0
servers:
  - url: https://api.example.com/v1
paths:
  /things:
    get:
      operationId: listThings
      tags: [things]
      responses:
        "200": { description: ok }
        "401": { description: unauthorized }
    post:
      operationId: createThing
      responses:
        "201": { description: created }
  /things/{id}:
    delete:
      operationId: deleteThing
      security: []
      responses:
        "204": { description: gone }
    parameters:
      - name: id
        in: path
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
  schemas:
    Thing:
      type: object
      required: [id, name]
security:
  - bearerAuth: [things:read]
`))
	facts.Normalize()

	if facts.Module.Name != "Thing API" || facts.Module.Version != "1.2.0" {
		t.Errorf("module = %+v", facts.Module)
	}
	if facts.Module.LangVersion != "3.1.0" {
		t.Errorf("spec version = %q", facts.Module.LangVersion)
	}
	serviceOf(t, facts, "https://api.example.com/v1")

	get := contractOf(t, facts, "GET /things")
	if get.Kind != "endpoint" || get.Package != "things" {
		t.Errorf("get = %+v", get)
	}
	// The operation id is what a generated client names its method, and the response
	// codes are the contract's observable surface.
	if get.Detail != "listThings -> 200,401" {
		t.Errorf("detail = %q", get.Detail)
	}
	contractOf(t, facts, "POST /things")
	contractOf(t, facts, "DELETE /things/{id}")
	// `parameters` is a path-level key, not an operation. Reading it as one would
	// invent a route that does not exist.
	for _, c := range facts.Contracts {
		if c.Name == "PARAMETERS /things/{id}" {
			t.Error("a path-level key must not become an endpoint")
		}
	}
	// A schema is a shared type the endpoints exchange; its required fields are the
	// part a consumer breaks on.
	if s := contractOf(t, facts, "Thing"); s.Kind != "schema" || s.Detail != "id,name" {
		t.Errorf("schema = %+v", s)
	}
	// The authentication surface: which credential kinds this API accepts, by name.
	// There is no value in a schema to leak.
	secretRefOf(t, facts, "bearerAuth", "http/bearer")
	if r := secretRefOf(t, facts, "bearerAuth", "things:read"); r.EnvVar != "" {
		t.Errorf("a document-level requirement applies to every operation: %+v", r)
	}
}

func TestOpenAPIJSONInAYAMLFile(t *testing.T) {
	// A .yaml file holding JSON is common enough that name-based dispatch would misread
	// real schemas; the first non-space byte settles it.
	facts := ExtractOpenAPI(contractFile("api/openapi.yaml", `{
  "openapi": "3.0.3",
  "info": {"title": "J", "version": "1"},
  "paths": {"/x": {"get": {"operationId": "getX", "responses": {"200": {}}}}}
}`))
	facts.Normalize()
	if facts.Incomplete {
		t.Errorf("unexpected incompleteness: %s", facts.Note)
	}
	if got := contractOf(t, facts, "GET /x").Detail; got != "getX -> 200" {
		t.Errorf("detail = %q", got)
	}
}

func TestSwagger2Extraction(t *testing.T) {
	facts := ExtractOpenAPI(contractFile("swagger.yaml", `
swagger: "2.0"
info: { title: Old, version: "1" }
host: api.example.com
basePath: /v2
paths:
  /ping: { get: { operationId: ping, responses: { "200": {} } } }
securityDefinitions:
  apiKey: { type: apiKey, name: X-Api-Key, in: header }
definitions:
  Pong: { type: object }
`))
	facts.Normalize()
	// Swagger 2.0 splits into host + basePath what OpenAPI 3 joins into one URL.
	serviceOf(t, facts, "api.example.com/v2")
	contractOf(t, facts, "GET /ping")
	contractOf(t, facts, "Pong")
	secretRefOf(t, facts, "apiKey", "apiKey")
}

// A YAML file under api/ may be anything. Empty facts are the honest reading, and no
// diagnostic, because nothing was missed.
func TestNonOpenAPIYAMLYieldsNothing(t *testing.T) {
	facts := ExtractOpenAPI(contractFile("api/notes.yaml", "paths:\n  - a\n  - b\n"))
	if len(facts.Contracts) != 0 || facts.Incomplete {
		t.Errorf("facts = %+v, want empty and unflagged", facts)
	}
}

func TestOpenAPIWithNoOperationsIsIncomplete(t *testing.T) {
	facts := ExtractOpenAPI(contractFile("api/openapi.yaml", "openapi: 3.1.0\ninfo: { title: Empty }\n"))
	if !facts.Incomplete {
		t.Error("an OpenAPI document with no paths or schemas should be reported as unread")
	}
}

func TestGraphQLExtraction(t *testing.T) {
	facts := ExtractGraphQL(contractFile("schema.graphql", `
"""
The root query. See https://example.com/#docs for details.
"""
type Query {
  thing(id: ID!): Thing
  things(first: Int = 10): [Thing!]!
}

type Mutation {
  createThing(input: CreateThingInput!): Thing!  # returns the new thing
}

interface Node {
  id: ID!
}

type Thing implements Node {
  id: ID!
  name: String!
  status: Status
}

enum Status {
  READY
  PENDING
}

input CreateThingInput {
  name: String!
}

union Result = Thing | Error

scalar DateTime

extend type Query {
  search(q: String!): [Result!]!
}
`))
	facts.Normalize()

	q := contractOf(t, facts, "Query", "type")
	if q.Detail != "thing, things" {
		t.Errorf("Query = %+v", q)
	}
	// A `#` inside a description is content, not a comment, and a trailing comment on a
	// field line is not part of the field.
	if got := contractOf(t, facts, "Mutation").Detail; got != "createThing" {
		t.Errorf("Mutation = %q", got)
	}
	if contractOf(t, facts, "Node").Kind != "interface" {
		t.Error("Node should be an interface")
	}
	if got := contractOf(t, facts, "Thing").Detail; got != "id, name, status" {
		t.Errorf("Thing = %q", got)
	}
	// An enum value is what a client sends; removing one breaks it at run time.
	if got := contractOf(t, facts, "Status").Detail; got != "READY, PENDING" {
		t.Errorf("Status = %q", got)
	}
	if contractOf(t, facts, "CreateThingInput").Kind != "input" {
		t.Error("CreateThingInput should be an input")
	}
	if contractOf(t, facts, "DateTime").Kind != "scalar" {
		t.Error("DateTime should be a scalar")
	}
	// A union's members are its contract: a client matching on one breaks if it goes.
	if got := contractOf(t, facts, "Result").Detail; got != "Thing, Error" {
		t.Errorf("Result = %q", got)
	}
	// An extension is additive to a type declared elsewhere, possibly in another file,
	// which is a different fact from declaring it — so it stays a separate contract
	// rather than merging into the base.
	if e := contractOf(t, facts, "Query", "extend-type"); e.Detail != "search" {
		t.Errorf("extend = %+v", e)
	}
	var sawExtend bool
	for _, c := range facts.Contracts {
		if c.Kind == "extend-type" && c.Name == "Query" && c.Detail == "search" {
			sawExtend = true
		}
	}
	if !sawExtend {
		t.Error("the extend block was not recorded separately")
	}
}

func TestGraphQLSingleLineType(t *testing.T) {
	facts := ExtractGraphQL(contractFile("s.graphql", "type Ping { ok: Boolean! }\ntype Pong { ok: Boolean! }\n"))
	facts.Normalize()
	if got := contractOf(t, facts, "Ping").Detail; got != "ok" {
		t.Errorf("Ping = %q", got)
	}
	if got := contractOf(t, facts, "Pong").Detail; got != "ok" {
		t.Errorf("Pong = %q", got)
	}
}

func TestGraphQLEmptyIsIncomplete(t *testing.T) {
	facts := ExtractGraphQL(contractFile("s.graphql", "# just a comment\n"))
	if !facts.Incomplete {
		t.Error("a schema with no type definitions should be reported as unread")
	}
}

func TestContractExtractionIsDeterministic(t *testing.T) {
	cases := []struct {
		f  discover.File
		fn func(discover.File) Facts
	}{
		{contractFile("a.proto", "package p;\nservice B { rpc Z(X) returns (Y); }\nservice A { rpc Q(X) returns (Y); }\nmessage M { string s = 1; }\n"), ExtractProto},
		{contractFile("o.yaml", "openapi: 3.1.0\npaths:\n  /z: { get: { responses: { \"200\": {} } } }\n  /a: { post: { responses: { \"201\": {} } } }\n"), ExtractOpenAPI},
		{contractFile("s.graphql", "type Z { b: Int\n a: Int }\ntype A { y: Int }\n"), ExtractGraphQL},
	}
	for _, c := range cases {
		first := ""
		for i := 0; i < 10; i++ {
			facts := c.fn(c.f)
			facts.Normalize()
			got := renderFacts(facts)
			if i == 0 {
				first = got
				continue
			}
			if got != first {
				t.Fatalf("%s run %d differed", c.f.Path, i)
			}
		}
	}
}
