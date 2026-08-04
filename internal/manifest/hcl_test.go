package manifest

import (
	"strings"
	"testing"
)

// The four places a naive brace count goes wrong, each asserted on the same observable:
// how many top-level blocks came out. A parser that miscounts braces does not report an
// error — it silently reparents the rest of the file, which is why every case here checks
// the structure and not a diagnostic.
func TestParseHCLTracksBlockStructure(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		blocks []string // top-level block kinds, in order
	}{
		{
			name: "nested blocks stay nested",
			src: `
resource "aws_instance" "web" {
  lifecycle {
    ignore_changes = [tags]
  }
  root_block_device {
    volume_size = 50
  }
}
output "id" {
  value = 1
}
`,
			blocks: []string{"resource", "output"},
		},
		{
			name: "a brace inside a string is not structure",
			src: `
variable "template" {
  default = "{ not a block }"
}
variable "second" {
  default = "}"
}
`,
			blocks: []string{"variable", "variable"},
		},
		{
			// The nested quotes are what this case is really about. A scanner that ends
			// the string at the first `"` it meets ends it inside the interpolation, and
			// from there every quote in the file is inverted — the first fixture written
			// here used `${lower({a = 1}["a"])}` and passed either way, because its braces
			// happened to balance and the halves reconcatenated. The unmatched `{` inside
			// the inner string is what makes the two behaviours distinguishable.
			name: "a brace inside an interpolated string is not structure",
			src: `
resource "aws_s3_bucket" "b" {
  bucket = "${format("{%s", var.env)}-assets"
  policy = "${lower({a = 1}["a"])}"
}
output "done" {
  value = true
}
`,
			blocks: []string{"resource", "output"},
		},
		{
			name: "a brace inside a heredoc is not structure",
			src: `
resource "aws_instance" "web" {
  user_data = <<-EOT
    #!/bin/sh
    if true; then { echo hi; } fi
    printf '%s' "}"
  EOT
  instance_type = "t3.micro"
}
output "done" {
  value = true
}
`,
			blocks: []string{"resource", "output"},
		},
		{
			// The leading comment covers statement position, which is header's business
			// and not skipBlank's — the two used to both handle it and neither could be
			// shown to matter. The `// }` and `/* } */` cover a comment after an
			// attribute and one spanning lines inside a block.
			name: "a brace inside a comment is not structure",
			src: `
# resource "aws_instance" "commented" {
variable "real" {
  # }
  type = string // }
  /* }
     still a comment } */
}
output "done" {
  value = true
}
`,
			blocks: []string{"variable", "output"},
		},
		{
			// An escaped quote is the same trap as the interpolation above, and it hides
			// the same way: `escaped = "a \"quoted\" word"` read without escape handling
			// splits into `"a \"` and `" word"`, which reconcatenate into the identical
			// value text, so asserting the value proves nothing. What distinguishes the
			// two readings is a character the value scanner treats as structure sitting
			// between the halves — the `{` here. Lose the escape and the string ends
			// early, the brace opens a bracket depth that eats the rest of the block
			// including its closing brace, and `output` is read as nested inside
			// `variable`.
			name: "an escaped quote before a brace does not end the string",
			src: `
variable "quoted" {
  default = "a brace \"{\" inside a string"
  type    = string
}
output "done" {
  value = true
}
`,
			blocks: []string{"variable", "output"},
		},
		{
			// A block label holding a character the header scanner treats as syntax. The
			// quotes are what protect it, and without them the `=` ends the header as an
			// attribute and the `#` starts a comment that eats the rest of the line.
			name: "syntax characters inside a quoted label are not syntax",
			src: `
resource "aws_instance" "web#1" {
  instance_type = "t3.micro"
}
resource "aws_instance" "a=b" {
  instance_type = "t3.micro"
}
output "done" {
  value = true
}
`,
			blocks: []string{"resource", "resource", "output"},
		},
		{
			name: "a multi-line list is one value",
			src: `
variable "azs" {
  default = [
    "us-east-1a",
    "us-east-1b",
  ]
  type = list(string)
}
output "done" {
  value = true
}
`,
			blocks: []string{"variable", "output"},
		},
		{
			name: "an object value spanning lines is one value",
			src: `
locals {
  tags = {
    env  = "prod"
    team = "platform"
  }
  name = "app"
}
output "done" {
  value = true
}
`,
			blocks: []string{"locals", "output"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, diag := parseHCL(tt.src)
			var got []string
			for _, b := range root.blocks {
				got = append(got, b.kind)
			}
			if strings.Join(got, ",") != strings.Join(tt.blocks, ",") {
				t.Fatalf("top-level blocks = %v, want %v (notes: %v)", got, tt.blocks, diag.Notes)
			}
			if diag.Malformed {
				t.Errorf("valid HCL reported as malformed: %v", diag.Notes)
			}
		})
	}
}

// A nested block's attributes belong to it and not to its parent. The failure this guards
// is silent and specific: `volume_size` read as the instance's own would put a resource's
// disk size on the resource, which is wrong in a way nothing downstream can detect.
func TestParseHCLKeepsNestedAttributesOutOfTheParent(t *testing.T) {
	root, _ := parseHCL(`
resource "aws_instance" "web" {
  instance_type = "t3.micro"
  root_block_device {
    volume_size = 50
    encrypted   = true
  }
}
`)
	if len(root.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(root.blocks))
	}
	res := root.blocks[0]
	if got := res.stringAttr("instance_type"); got != "t3.micro" {
		t.Errorf("instance_type = %q", got)
	}
	if _, ok := res.attr("volume_size"); ok {
		t.Error("volume_size read as the resource's own attribute; it belongs to root_block_device")
	}
	if len(res.blocks) != 1 || res.blocks[0].kind != "root_block_device" {
		t.Fatalf("nested blocks = %+v", res.blocks)
	}
	if !res.blocks[0].boolAttr("encrypted") {
		t.Error("encrypted not read on the nested block")
	}
}

// stringAttr answers "did the author state this literally", and an expression is not a
// stated fact. The negative rows are the load-bearing ones: a reader that returned the
// expression text as a value would record `var.bucket_name` as a bucket named
// "var.bucket_name".
func TestStringAttrReturnsLiteralsOnly(t *testing.T) {
	root, _ := parseHCL(`
block "b" {
  plain       = "value"
  escaped     = "a \"quoted\" word"
  newline     = "line\nbreak"
  empty       = ""
  interpolated = "${var.env}-api"
  expression  = var.bucket_name
  concatenated = "a-" + var.suffix
  number      = 42
  boolish     = true
  func        = lower("ABC")
  listed      = ["a"]
}
`)
	b := root.blocks[0]
	for _, tt := range []struct{ key, want string }{
		{"plain", "value"},
		{"escaped", `a "quoted" word`},
		{"newline", "line\nbreak"},
		{"empty", ""},
		// An interpolation is kept whole. It is a fact about the value — this name is
		// environment-suffixed — and a reader handed "-api" could not tell that.
		{"interpolated", "${var.env}-api"},
		{"expression", ""},
		{"concatenated", ""},
		{"number", ""},
		{"boolish", ""},
		{"func", ""},
		{"listed", ""},
		{"absent", ""},
	} {
		if got := b.stringAttr(tt.key); got != tt.want {
			t.Errorf("stringAttr(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}

	// boolAttr is the literal `true` and nothing else. A conditional sensitivity reads as
	// false, which is the safe direction: no value is read from any variable regardless.
	if !b.boolAttr("boolish") {
		t.Error("boolAttr(boolish) = false, want true")
	}
	for _, key := range []string{"plain", "expression", "number", "absent"} {
		if b.boolAttr(key) {
			t.Errorf("boolAttr(%q) = true", key)
		}
	}
}

// hclObjectField reads a field out of an object literal, which is the shape
// required_providers uses. The negatives matter because the same syntax position can hold
// a variable reference or a merge() call, and neither states a version.
func TestHCLObjectField(t *testing.T) {
	tests := []struct {
		name  string
		value string
		field string
		want  string
	}{
		{"inline", `{ source = "hashicorp/aws", version = "~> 5.0" }`, "version", "~> 5.0"},
		{"newline separated", "{\n source = \"hashicorp/aws\"\n version = \"~> 5.0\"\n}", "version", "~> 5.0"},
		{"newline separated source", "{\n source = \"hashicorp/aws\"\n version = \"~> 5.0\"\n}", "source", "hashicorp/aws"},
		{"absent field", `{ source = "hashicorp/aws" }`, "version", ""},
		{"nested object does not confuse the split", `{ source = "s", meta = { version = "inner" }, version = "outer" }`, "version", "outer"},
		{"a string holding a comma", `{ source = "a,b", version = "1" }`, "source", "a,b"},
		{"not an object", `var.providers`, "version", ""},
		{"a function call", `merge(local.base, { version = "1" })`, "version", ""},
		{"a list", `["a", "b"]`, "version", ""},
		{"an expression value", `{ version = var.v }`, "version", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hclObjectField(tt.value, tt.field); got != tt.want {
				t.Errorf("hclObjectField(%q, %q) = %q, want %q", tt.value, tt.field, got, tt.want)
			}
		})
	}
}

// splitHCLHeader separates a block's type from its labels. Unquoted labels are accepted
// because a file that writes them is a file whose resources are still worth knowing about.
func TestSplitHCLHeader(t *testing.T) {
	tests := []struct {
		head   string
		kind   string
		labels []string
	}{
		{`resource "aws_s3_bucket" "logs" `, "resource", []string{"aws_s3_bucket", "logs"}},
		{`module "vpc" `, "module", []string{"vpc"}},
		{`terraform `, "terraform", nil},
		{`resource aws_s3_bucket logs `, "resource", []string{"aws_s3_bucket", "logs"}},
		{`resource "aws_s3_bucket with space" "logs" `, "resource", []string{"aws_s3_bucket with space", "logs"}},
		{`   `, "", nil},
	}
	for _, tt := range tests {
		kind, labels := splitHCLHeader(tt.head)
		if kind != tt.kind || strings.Join(labels, "|") != strings.Join(tt.labels, "|") {
			t.Errorf("splitHCLHeader(%q) = %q, %v; want %q, %v", tt.head, kind, labels, tt.kind, tt.labels)
		}
	}
}

// A broken file yields what came before the break plus a diagnostic saying so, which is the
// Reader contract in registry.go: a partial reading is the normal outcome and an error would
// discard it. The distinction between `Malformed` and an ordinary note is the one that
// matters — see the Diag comment for why issue #9 exists.
func TestParseHCLReportsWhatItCouldNotRead(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		malformed bool
		wantNote  string
		blocks    int
	}{
		{
			name:      "unterminated block",
			src:       "variable \"a\" {\n  type = string\n\nresource \"r\" \"n\" {\n",
			malformed: true,
			wantNote:  "unterminated block",
			blocks:    1,
		},
		{
			name:      "unterminated string",
			src:       "variable \"a\" {\n  default = \"open\n}\n",
			malformed: true,
			wantNote:  "unterminated string",
			blocks:    1,
		},
		{
			name:      "unterminated heredoc",
			src:       "resource \"r\" \"n\" {\n  user_data = <<-EOT\n    hello\n}\n",
			malformed: true,
			wantNote:  "unterminated heredoc EOT",
			blocks:    1,
		},
		{
			name:      "unterminated block comment",
			src:       "variable \"a\" {\n  /* forever\n}\n",
			malformed: true,
			wantNote:  "unterminated block comment",
			blocks:    1,
		},
		{
			name:      "unmatched closing brace",
			src:       "}\nvariable \"a\" {\n  type = string\n}\n",
			malformed: true,
			wantNote:  "unmatched closing brace",
			blocks:    1,
		},
		{
			name:      "a computed assignment target is unread, not malformed",
			src:       "resource \"r\" \"n\" {\n  tags[\"env\"] = \"prod\"\n}\n",
			malformed: false,
			wantNote:  "computed name",
			blocks:    1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, diag := parseHCL(tt.src)
			if !diag.Incomplete() {
				t.Fatal("nothing reported; silence would present a partial reading as complete")
			}
			if diag.Malformed != tt.malformed {
				t.Errorf("malformed = %v, want %v (notes: %v)", diag.Malformed, tt.malformed, diag.Notes)
			}
			if !strings.Contains(diag.Summary(), tt.wantNote) {
				t.Errorf("summary = %q, want it to mention %q", diag.Summary(), tt.wantNote)
			}
			// The partial reading is kept: that is why a Reader returns facts rather
			// than an error.
			if len(root.blocks) != tt.blocks {
				t.Errorf("blocks = %d, want %d; the readable part must survive", len(root.blocks), tt.blocks)
			}
		})
	}
}

// A file of nothing but open braces must not take the process down. This is reachable by
// anyone who can add a file to a repository signpost runs over, which makes an unbounded
// recursion a denial of service rather than a style problem.
func TestParseHCLBoundsNesting(t *testing.T) {
	src := "root {\n" + strings.Repeat("nested {\n", 5000)
	root, diag := parseHCL(src)
	if len(root.blocks) != 1 || root.blocks[0].kind != "root" {
		t.Fatalf("blocks = %+v", root.blocks)
	}
	if !strings.Contains(diag.Summary(), "nested deeper than the reader follows") {
		t.Errorf("summary = %q; a depth the reader gave up on must be said out loud", diag.Summary())
	}
	// Past the cap the block is consumed by counting braces rather than parsed, and that
	// skip has to land on the matching close. A skip that stops one brace early leaves
	// every following `}` closing the wrong thing, and the file after it is read as
	// nested inside the block that was given up on.
	balanced := "root {\n" + strings.Repeat("nested {\n", 200) + strings.Repeat("}\n", 200) + "}\nvariable \"after\" {\n type = string\n}\n"
	root2, diag2 := parseHCL(balanced)
	var kinds []string
	for _, b := range root2.blocks {
		kinds = append(kinds, b.kind)
	}
	if strings.Join(kinds, ",") != "root,variable" {
		t.Fatalf("blocks = %v, want root and variable; the skip lost its place", kinds)
	}
	// The file is valid — deeply nested is not broken — so giving up on depth is a note
	// and nothing more. `Malformed` says a conforming reader would lose data, and
	// claiming that of a well-formed file is the false report ADR 0001 is about.
	if diag2.Malformed {
		t.Errorf("a valid deeply-nested file reported as malformed: %v", diag2.Notes)
	}
	if len(diag2.Notes) != 1 {
		t.Errorf("notes = %v, want exactly the depth note; a second note means the skip desynchronised", diag2.Notes)
	}
	// The block that was skipped is still a block, with its header read. Reporting the
	// depth and then dropping the resource would lose the one fact that survived.
	if len(root2.blocks[0].blocks) != 1 || root2.blocks[0].blocks[0].kind != "nested" {
		t.Errorf("root's children = %+v, want the first nested block kept", root2.blocks[0].blocks)
	}
}

// The reader is deterministic byte-for-byte across readings, which is what keeps the
// committed bundle from churning on a cosmetic edit (design §8.1).
func TestExtractTerraformIsDeterministic(t *testing.T) {
	src := `
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}
module "b" { source = "./b" }
module "a" { source = "./a" }
resource "aws_ecs_service" "z" { name = "z" }
resource "aws_ecs_service" "a" { name = "a" }
variable "db_password" { sensitive = true }
`
	f := tfFile("infra/main.tf", src)
	first := ExtractTerraform(f)
	first.Normalize()
	second := ExtractTerraform(f)
	second.Normalize()
	if renderFacts(first) != renderFacts(second) {
		t.Fatalf("two readings differ:\n%s\n---\n%s", renderFacts(first), renderFacts(second))
	}
	// Sorted by identity, so reordering the file does not reorder the output.
	reordered := ExtractTerraform(tfFile("infra/main.tf", `
variable "db_password" { sensitive = true }
resource "aws_ecs_service" "a" { name = "a" }
resource "aws_ecs_service" "z" { name = "z" }
module "a" { source = "./a" }
module "b" { source = "./b" }
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}
`))
	reordered.Normalize()
	// Line numbers legitimately differ; everything else must not.
	if depNames(first) != depNames(reordered) {
		t.Errorf("deps depend on block order: %q vs %q", depNames(first), depNames(reordered))
	}
	if svcNames(first) != svcNames(reordered) {
		t.Errorf("services depend on block order: %q vs %q", svcNames(first), svcNames(reordered))
	}
}

func depNames(f Facts) string { return strings.Join(f.DepNames(), ",") }

func svcNames(f Facts) string {
	out := make([]string, 0, len(f.Services))
	for _, s := range f.Services {
		out = append(out, s.Name)
	}
	return strings.Join(sortedUnique(out), ",")
}
