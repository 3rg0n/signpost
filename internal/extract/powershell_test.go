package extract

import (
	"sort"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func psFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangPowerShell, Class: discover.ClassSource, Content: src,
	}
}

func extractPS(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := PowerShellExtractor{}.Extract(psFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real PowerShell, including the forms no other
// language here has: an Export-ModuleMember list that overrides every naming convention,
// comment-based help inside the function body, a `#Requires` comment that is code, and a
// here-string whose body is indistinguishable from source.
func psCorpus() []Fixture {
	return []Fixture{
		{
			File: psFile("src/Deploy/Deploy.psm1", `#Requires -Version 7.0
#Requires -Modules Pester, powershell-yaml

using namespace System.Text
using module ./Widget.psm1

Import-Module Az.Storage -MinimumVersion 5.0
Import-Module "$PSScriptRoot/lib/Logging.psm1"

Set-StrictMode -Version Latest

function Get-Artifact {
    <#
    .SYNOPSIS
    Fetches a build artifact by name.
    .PARAMETER Name
    The artifact's name.
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string]$Name,
        [int]$Retries = 3
    )
    Write-Verbose "fetching $Name"
}

# Publishes an artifact to the registry.
function Publish-Artifact {
    param([string]$Path)
    Write-Output $Path
}

filter Where-Recent {
    $_ | Where-Object { $_.Age -lt 7 }
}

function Get-InternalState {
    param()
    @{ ok = $true }
}

Export-ModuleMember -Function Get-Artifact, Publish-Artifact -Alias ga
`),
			Expected: Expected{
				Imports: []string{
					"./Widget.psm1", "./lib/Logging.psm1", "Az.Storage", "Pester",
					"System.Text", "powershell-yaml",
				},
				Symbols: []string{
					"Get-Artifact", "Get-InternalState", "Publish-Artifact", "Where-Recent",
				},
				// Export-ModuleMember is authoritative. Where-Recent and Get-InternalState
				// have no leading underscore and would be public under the convention rule,
				// and the module's own statement says they are not — which is the whole
				// reason exportedness cannot be decided while walking declarations.
				Exported:    []string{"Get-Artifact", "Publish-Artifact"},
				Entrypoints: []string{},
			},
		},
		{
			// A class module. PowerShell classes are shared only by `using module` — an
			// Import-Module does not bring them across — so this is where a repository's
			// real internal edges are written. No Export-ModuleMember, so the underscore
			// convention decides for the functions.
			File: psFile("src/Deploy/Widget.psm1", `using namespace System.Collections.Generic

class Widget {
    [string]$Name
    [int]$Count = 0
    static [Widget]$Empty
    hidden [hashtable]$cache = @{}
    $Metadata

    Widget([string]$name) {
        $this.Name = $name
    }

    [string] Describe() {
        return "$($this.Name) x $($this.Count)"
    }

    static [Widget] Create([string]$name) {
        return [Widget]::new($name)
    }

    hidden [void] Reset() {
        $this.cache.Clear()
    }
}

enum Severity {
    Low
    High
}

function New-Widget {
    param([string]$Name)
    [Widget]::Create($Name)
}

function _assert-Widget {
    param($w)
    if (-not $w) { throw "null" }
}
`),
			Expected: Expected{
				Imports: []string{"System.Collections.Generic"},
				Symbols: []string{
					"New-Widget", "Severity", "Widget", "Widget.Count", "Widget.Create",
					"Widget.Describe", "Widget.Empty", "Widget.Metadata", "Widget.Name",
					"Widget.Reset", "Widget.cache", "_assert-Widget",
				},
				// `hidden` is the only visibility keyword the language has and its default is
				// the inverse of C#'s: a member with no modifier is public. `static` says
				// nothing about visibility, so Create and Empty are surface exactly as
				// Describe and Name are, and $Metadata is surface without a type annotation
				// because PowerShell's is optional. Severity's cases are not recorded,
				// matching what the C# extractor does with an enum, and the constructor is
				// skipped for the reason javaMethodDecl gives.
				Exported: []string{
					"New-Widget", "Severity", "Widget", "Widget.Count", "Widget.Create",
					"Widget.Describe", "Widget.Empty", "Widget.Metadata", "Widget.Name",
				},
				Entrypoints: []string{},
			},
		},
		{
			// A script rather than a module: a top-level param block is what makes it one,
			// and dot-sourcing is how a `.ps1` loads a library. Windows separators appear
			// because real scripts are written with them.
			File: psFile("scripts/Invoke-Build.ps1", `#!/usr/bin/env pwsh
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Configuration,
    [switch]$Clean
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

. "$PSScriptRoot\lib\Logging.ps1"
. "$PSScriptRoot/lib/Retry.ps1"

function Invoke-Compile {
    param([string]$Config)
    Write-Log "compiling $Config"
}

function Script:Get-Timestamp {
    (Get-Date).ToString('o')
}

Invoke-Compile -Config $Configuration
`),
			Expected: Expected{
				Imports: []string{"./lib/Logging.ps1", "./lib/Retry.ps1"},
				// The scope qualifier is stripped: `Script:Get-Timestamp` declares
				// Get-Timestamp, and recording the qualifier would put a name on the page
				// that no caller writes.
				Symbols:  []string{"Get-Timestamp", "Invoke-Compile"},
				Exported: []string{"Get-Timestamp", "Invoke-Compile"},
				// Both signals: the shebang makes it executable on Linux, and the param block
				// is how a .ps1 declares the arguments it is invoked with.
				Entrypoints: []string{"#!", "param"},
			},
		},
		{
			// The adversarial fixture. Everything that looks like a declaration and is not: a
			// here-string body, a block comment, an interpolated string, a nested function
			// (which PowerShell scopes to its parent, unlike the shell), a `using:` scope
			// modifier, and a `-Force` switch that must not be read as a module name.
			File: psFile("scripts/Tricky.ps1", `<#
Import-Module Ghost.Commented
function Get-BlockCommentGhost { }
#>

# Import-Module Ghost.LineCommented

Import-Module -Name Pester -Force -MinimumVersion 5.0

function Get-Real {
    param([string]$Name)

    $sql = @"
Import-Module Ghost.HereString
function Get-HereStringGhost {
    Write-Output "not code"
}
"@

    $literal = @'
function Get-LiteralGhost { }
'@

    Invoke-RestMethod -Uri $u -Body @"
Import-Module Ghost.Body
"@ -ContentType 'application/json'

    Write-Output "Import-Module Ghost.Interpolated"
    Write-Output 'function Get-QuotedGhost { }'

    # A nested function is scoped to Get-Real and vanishes when it returns — the
    # opposite of the shell, where a nested definition is global.
    function Get-Nested {
        Write-Output "inner"
    }

    Invoke-Command -ScriptBlock { Write-Output $using:Name }

    $splat = @{ Name = $Name }
    Get-ChildItem @splat

    if ($Name -match '^\w+$') { Write-Output "ok" }
    foreach ($x in 1..3) { Write-Output $x }
    switch ($Name) { 'a' { 1 } default { 0 } }
}

function Get-AfterTheTricks {
    Write-Output "reached"
}
`),
			Expected: Expected{
				// Only Pester. `-Force` is a switch and `-MinimumVersion 5.0` is a version, and
				// reading either as a module would put a package in the graph that no gallery
				// holds.
				Imports:     []string{"Pester"},
				Symbols:     []string{"Get-AfterTheTricks", "Get-Real"},
				Exported:    []string{"Get-AfterTheTricks", "Get-Real"},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises for PowerShell.
func TestPowerShellExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(PowerShellExtractor{}, discover.LangPowerShell, psCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("PowerShell extractor below target:\n%s", ls.Report())
	}
	t.Logf("PowerShell extractor score:\n%s", ls.Report())
}

// Export-ModuleMember is the one export statement in this package that has to be read
// before any symbol can be judged, and both directions matter. A module that calls it
// exports only what it names; a module that does not exports everything. Collapsing the two
// would report a module's internals as its public surface — and this is the assertion that
// distinguishes reading the statement from ignoring it, because the convention rule alone
// would call every one of these functions public.
func TestPowerShellExportModuleMemberIsAuthoritative(t *testing.T) {
	listed := extractPS(t, "M.psm1", `function Get-One { }
function Get-Two { }
function Get-Three { }

Export-ModuleMember -Function Get-One, Get-Two
`)
	if got := strings.Join(exportedNames(listed), ","); got != "Get-One,Get-Two" {
		t.Errorf("exported = %q; a function absent from the list is private however it is named", got)
	}
	// The absence case. Without the statement every function is exported, so the underscore
	// convention is all that decides.
	silent := extractPS(t, "N.psm1", `function Get-One { }
function Get-Two { }
function _Get-Helper { }
`)
	if got := strings.Join(exportedNames(silent), ","); got != "Get-One,Get-Two" {
		t.Errorf("exported = %q; with no statement, the convention decides", got)
	}
}

// The array form and the wildcard form. An array is what a generated module manifest emits,
// and a wildcard exports by pattern — which cannot be evaluated here, so it falls back to
// the default rather than exporting nothing. Falling back to *nothing* would report a whole
// module as having no public surface, which is a worse error than the default's.
func TestPowerShellExportModuleMemberArrayAndWildcard(t *testing.T) {
	arr := extractPS(t, "M.psm1", `function Get-One { }
function Get-Two { }
function Get-Three { }

Export-ModuleMember -Function @('Get-One','Get-Two') -Variable Config
`)
	if got := strings.Join(exportedNames(arr), ","); got != "Get-One,Get-Two" {
		t.Errorf("exported = %q; the quoted array form names the same set", got)
	}
	wild := extractPS(t, "N.psm1", `function Get-One { }
function _Get-Helper { }

Export-ModuleMember -Function *-Thing
`)
	if got := strings.Join(exportedNames(wild), ","); got != "Get-One" {
		t.Errorf("exported = %q; a wildcard is unevaluable, so the convention decides", got)
	}
}

// A nested function is scoped to its parent and disappears when the parent returns. This is
// the single clearest reason shell and PowerShell are two extractors rather than one: the
// shell's nested definition is global, so ShellExtractor deliberately has no declaration
// site rule at all, and applying the shell's reading here would put a name on the page that
// nothing outside the enclosing function can call.
func TestPowerShellNestedFunctionIsNotSurface(t *testing.T) {
	fa := extractPS(t, "a.ps1", `function Get-Outer {
    function Get-Inner {
        Write-Output "inner"
    }
    Get-Inner
}

function Get-Sibling { }
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Get-Outer,Get-Sibling" {
		t.Errorf("symbols = %q; a nested function is scoped to its parent", got)
	}
}

// The three import forms are unrelated dependencies and only one of them can ever resolve
// to a node here. A path names a file in this repository; a module name names a gallery
// module; a namespace names .NET. Collapsing them would either invent a package or lose a
// real internal edge.
func TestPowerShellThreeImportFormsStayDistinct(t *testing.T) {
	fa := extractPS(t, "src/a.psm1", `using namespace System.Text
using module ./Widget.psm1
Import-Module Pester
Import-Module "$PSScriptRoot/lib/Log.psm1"
. "$PSScriptRoot\lib\Retry.ps1"
`)
	want := "./Widget.psm1,./lib/Log.psm1,./lib/Retry.ps1,Pester,System.Text"
	if got := strings.Join(fa.ImportPaths(), ","); got != want {
		t.Errorf("imports = %q, want %q", got, want)
	}
}

// A Windows separator is what real PowerShell is written with, even on Linux where the
// shell accepts it. Every path in the graph is `/`-separated, so a `\` reaching the resolver
// would never match a file and the edge would silently vanish.
func TestPowerShellBackslashPathsAreNormalized(t *testing.T) {
	fa := extractPS(t, "scripts/a.ps1", `. "$PSScriptRoot\lib\Logging.ps1"
Import-Module "$PSScriptRoot\..\src\Widget.psm1"
`)
	for _, p := range fa.ImportPaths() {
		if strings.Contains(p, `\`) {
			t.Errorf("import %q kept a backslash; the graph is slash-separated", p)
		}
	}
	if got := strings.Join(fa.ImportPaths(), ","); got != "./../src/Widget.psm1,./lib/Logging.ps1" {
		t.Errorf("imports = %q", got)
	}
}

// Import-Module's parameters are the negative boundary that matters most, because a switch
// and a version both sit exactly where the module name would. `-Force` read as a module
// would put a package named Force in the graph, and `-MinimumVersion 5.0` would add one
// named `5.0`.
func TestPowerShellImportModuleParametersAreNotModuleNames(t *testing.T) {
	fa := extractPS(t, "a.ps1", `Import-Module Pester -Force
Import-Module -Name Az.Storage -MinimumVersion 5.0 -Scope Global
Import-Module -Force -Name platyPS
ipmo PSReadLine
`)
	want := "Az.Storage,PSReadLine,Pester,platyPS"
	if got := strings.Join(fa.ImportPaths(), ","); got != want {
		t.Errorf("imports = %q, want %q; a switch is not a module", got, want)
	}
}

// `#Requires` is the one comment in this package read as code, because in PowerShell it is:
// the engine enforces it before the script runs, and a script declaring its modules that way
// declares them nowhere else.
//
// The negative half is the other four directives. -Version, -RunAsAdministrator and
// -PSEdition are execution preconditions, and reading their arguments as modules would put
// packages named `7.0` and `Core` in the graph that no gallery holds.
func TestPowerShellRequiresIsReadAndOnlyModulesAre(t *testing.T) {
	fa := extractPS(t, "a.ps1", `#Requires -Version 7.0
#Requires -Modules Pester, powershell-yaml
#Requires -RunAsAdministrator
#Requires -PSEdition Core
# Requires -Modules NotADirective
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "Pester,powershell-yaml" {
		t.Errorf("imports = %q; every required module is a dependency in its own right", got)
	}
	// Each is its own Import, not one Import carrying the other in Names. Names is the set of
	// symbols a specifier brings across, so parking the second module there would report a
	// dependency on Pester with a member named powershell-yaml — and the second module's edge
	// would never reach the graph, because nothing resolves a Name.
	for _, im := range fa.Imports {
		if len(im.Names) != 0 {
			t.Errorf("import %q carries Names %q; a required module is not a member of another",
				im.Raw, strings.Join(im.Names, ","))
		}
	}
}

// A computed path names a file nothing here can know. The anchored form is exact — unlike
// the shell's `$(dirname "$0")`, $PSScriptRoot *is* the running file's directory — so the
// positive case is a fact rather than a reading of intent; the negative case is what keeps
// an invented module out of the graph.
func TestPowerShellComputedPathIsNotInvented(t *testing.T) {
	fa := extractPS(t, "a.ps1", `. "$moduleRoot/$name.ps1"
Import-Module "$env:LIB_DIR/Thing.psm1"
using module @{ModuleName='Pinned';RequiredVersion='1.0'}
. "$PSScriptRoot/real.ps1"
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "./real.ps1" {
		t.Errorf("imports = %q; only the anchored literal is a fact", got)
	}
}

// A here-string's body is data. The `@"` opener and `"@` terminator are punctuation rather
// than an identifier, which is why they need their own scanner rule and cannot use the
// heredoc mechanism's uppercase convention.
func TestPowerShellHereStringBodyDeclaresNothing(t *testing.T) {
	fa := extractPS(t, "a.ps1", `function Get-Real {
    $sql = @"
Import-Module Ghost.Interpolating
function Get-Ghost { }
"@

    $literal = @'
Import-Module Ghost.Literal
function Get-LiteralGhost { }
'@
}

function Get-After { }
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "" {
		t.Errorf("imports = %q; a here-string body declares no dependency", got)
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "Get-After,Get-Real" {
		t.Errorf("symbols = %q", got)
	}
}

// The negative boundary of the here-string rule, and the one that costs a whole file when it
// is wrong: a scanner that reads an ordinary `@` as an opener blanks every line below it, so
// each declaration here vanishes with no error to read.
//
// The forms are chosen to hold the *end-of-line* requirement up, not just the sigil. `@"` and
// `@'` genuinely appear mid-line — `Get-Content @"C:\logs"` splats a path, and
// `-Filter @'x'@` is a literal — and it is only their position that says they are not
// openers. `@{`, `@(` and `@splat` are the sigil in its everyday places and are excluded a
// step earlier by the quote requirement, so they belong here too but cannot carry the guard
// alone: a test built from those three passes with the end-of-line check deleted.
func TestPowerShellAtSignIsNotAlwaysAHereString(t *testing.T) {
	fa := extractPS(t, "a.ps1", `function Get-Real {
    $table = @{ Name = 'x'; Count = 1 }
    $array = @(1, 2, 3)
    Get-ChildItem @table
    $mid = @"inline"@
    $lit = @'literal'@
    Write-Output @"a"@ -NoEnumerate
    $s = "a@b.com"
}

function Get-Below { }

class Marker {
    [string]$Field
}
`)
	want := "Get-Below,Get-Real,Marker,Marker.Field"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q; an `@` was read as a here-string opener, which blanks "+
			"every line below it and takes the declarations there out of the graph", got, want)
	}
}

// A here-string terminator closes as soon as it starts a line, and the rest of that line is
// code. This is the one rule where a here-string differs from every heredoc in this package
// rather than merely being spelled differently, and it is not an edge case: passing a
// here-string as a parameter value is most of the reason one is written, and the closing
// `"@ -ContentType 'application/json'` is what that looks like.
//
// Requiring the terminator to be the whole line leaves the string open to the end of the
// file, which is why the assertion is about the declarations below rather than about the
// string. Regression: found by probing the scanner after the mutation test on the opener
// guard survived.
func TestPowerShellHereStringCloserAllowsTrailingArguments(t *testing.T) {
	fa := extractPS(t, "a.ps1", `function Send-Payload {
    Invoke-RestMethod -Uri $u -Body @"
{"a":1}
"@ -ContentType 'application/json'
}

function Get-Query {
    $q = @'
SELECT 1
'@.Trim()
}

function Get-Below { }

class Marker {
    [string]$Field
}
`)
	want := "Get-Below,Get-Query,Marker,Marker.Field,Send-Payload"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q; a terminator with arguments after it left the "+
			"here-string open and blanked the rest of the file", got, want)
	}
}

// `hidden` is the language's only visibility keyword and its default is the inverse of C#'s:
// a class member with no modifier is public. Reading C#'s default here would report every
// PowerShell class in a codebase as having no members at all.
func TestPowerShellClassMembersArePublicUnlessHidden(t *testing.T) {
	fa := extractPS(t, "a.ps1", `class Widget {
    [string]$Name
    [int]$Count
    hidden [hashtable]$cache

    [string] Describe() { return $this.Name }
    static [Widget] Create() { return [Widget]::new() }
    hidden [void] Reset() { }
}
`)
	want := "Widget,Widget.Count,Widget.Create,Widget.Describe,Widget.Name,Widget.Reset,Widget.cache"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q", got, want)
	}
	// Sorted here because exportedNames renders in Facts.Symbols order, which sorts by the
	// bare name before the receiver is prefixed — so `Widget` follows its own members.
	got := exportedNames(fa)
	sort.Strings(got)
	if joined := strings.Join(got, ","); joined != "Widget,Widget.Count,Widget.Create,Widget.Describe,Widget.Name" {
		t.Errorf("exported = %q; only `hidden` removes a member from the surface", joined)
	}
}

// A method body's statements have a declaration's exact shapes — an assignment looks like a
// property and a call looks like a method — and the depth guard is the only thing excluding
// them, since nothing about the lines themselves says which side of the brace they are on.
func TestPowerShellClassMemberLookalikesDeclareNothing(t *testing.T) {
	fa := extractPS(t, "a.ps1", `class Widget {
    [string]$Real

    [void] Work() {
        $local = 1
        $this.Real = "x"
        Write-Output $local
        Get-ChildItem -Path "/tmp"
        if ($local -gt 0) { Write-Output "yes" }
    }
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Widget,Widget.Real,Widget.Work" {
		t.Errorf("symbols = %q; a statement in a method body is not a member", got)
	}
}

// A type annotation is optional on a property, because PowerShell's is: an undeclared type is
// [Object], and `class Widget { $Untyped }` is ordinary code. Requiring the bracket drops a
// legal member silently, and the class page then describes a type as having fewer fields than
// it has.
//
// `hidden $Private` is the pair that matters: with no annotation there is nothing between the
// keyword and the name, so a rule reading visibility from the text before the bracket has no
// bracket to work from.
//
// Regression: found by probing after the mutation on the annotation requirement survived.
func TestPowerShellUntypedPropertyIsStillAProperty(t *testing.T) {
	fa := extractPS(t, "a.ps1", `class Widget {
    $Untyped
    $WithDefault = 5
    [string]$Typed
    hidden $Private
    [void] Work() { }
}
`)
	want := "Widget,Widget.Private,Widget.Typed,Widget.Untyped,Widget.WithDefault,Widget.Work"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q; an untyped property is legal PowerShell", got, want)
	}
	got := exportedNames(fa)
	sort.Strings(got)
	wantExp := "Widget,Widget.Typed,Widget.Untyped,Widget.WithDefault,Widget.Work"
	if joined := strings.Join(got, ","); joined != wantExp {
		t.Errorf("exported = %q, want %q; `hidden` still applies with no annotation to sit "+
			"before", joined, wantExp)
	}
}

// A default value is cut before the line is judged, so a paren inside one does not disqualify
// the declaration it belongs to. `[int]$Count = Get-Count(5)` declares Count; a method's
// paren is in its declaration, which is what still tells the two apart.
//
// Regression: found by probing after the mutation on psMethodDecl's prefix guard survived —
// the guard is what keeps `Get-Count` off the page as a method of Widget.
func TestPowerShellPropertyDefaultHoldingACallStillDeclares(t *testing.T) {
	fa := extractPS(t, "a.ps1", `class Widget {
    [int]$Count = Get-Count(5)
    [string]$Name = [Widget]::Default()
    [hashtable]$Map = @{}
    [void] Work() { }
}
`)
	want := "Widget,Widget.Count,Widget.Map,Widget.Name,Widget.Work"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q; a call in a default is not a method of the class",
			got, want)
	}
}

// The keyword is case-insensitive because PowerShell is, and both capitalisations appear in
// real code. A case-sensitive rule would silently drop every function in a file that
// capitalises the keyword — a whole file's surface lost with no error to read.
func TestPowerShellKeywordsAreCaseInsensitive(t *testing.T) {
	fa := extractPS(t, "a.ps1", `Function Get-Upper { }
function Get-Lower { }
FUNCTION Get-Shouting { }
Filter Where-Upper { }
Class Thing {
    [string]$Field
}
Enum Level { Low }
functions_is_not_a_keyword = 1
`)
	want := "Get-Lower,Get-Shouting,Get-Upper,Level,Thing,Thing.Field,Where-Upper"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q", got, want)
	}
}

// Comment-based help lives *inside* the function body, which is where Get-Help looks first
// and where every template puts it — the opposite position from every other language here.
// The section keywords must not reach the page: a bundle page reading ".SYNOPSIS Fetches a
// thing." looks like a bug in the extracted prose.
//
// Get-CommentedMidBody is the boundary on the other side, and it is the reason the block has
// to be the body's *first* substantive line rather than one of its first few: a `<#` sitting
// below code is a remark about the statement it precedes, so reading it as help attributes a
// note about one line to the whole function. That is worse than reporting no doc, because it
// reads as authored. It has no comment above it either, so the fallback cannot supply one and
// the empty string is the whole answer. The known cost of the same rule is that help written
// after the `param()` block, which Get-Help also accepts, is not found — a limitation, not a
// near-miss, since nothing wrong is reported.
func TestPowerShellHelpIsReadFromInsideTheBody(t *testing.T) {
	fa := extractPS(t, "a.ps1", `function Get-Inside {
    <#
    .SYNOPSIS
    Fetches a thing by name.
    .PARAMETER Name
    The name.
    .EXAMPLE
    Get-Inside -Name x
    #>
    param([string]$Name)
}

<#
.SYNOPSIS
Documented from above.
#>
function Get-Above {
    param()
}

# Terse prose above.
function Get-Terse { }

function Get-Undocumented { }

function Get-CommentedMidBody {
    $x = 1
    <#
    .SYNOPSIS
    A remark about the line below, not help for this function.
    #>
    Write-Output $x
}
`)
	docs := map[string]string{}
	for _, s := range fa.Symbols {
		docs[s.Name] = s.Doc
	}
	for name, want := range map[string]string{
		"Get-Inside":           "Fetches a thing by name.",
		"Get-Above":            "Documented from above.",
		"Get-Terse":            "Terse prose above.",
		"Get-Undocumented":     "",
		"Get-CommentedMidBody": "",
	} {
		if docs[name] != want {
			t.Errorf("%s doc = %q, want %q", name, docs[name], want)
		}
	}
}

// A block comment and a quoted string both hold text that looks exactly like source, and in
// PowerShell the quoted case is routine: a script writes usage text naming its own commands.
func TestPowerShellCommentsAndStringsDeclareNothing(t *testing.T) {
	fa := extractPS(t, "a.ps1", `<#
Import-Module Ghost.Block
function Get-BlockGhost { }
#>

# Import-Module Ghost.Line

function Get-Real {
    Write-Output "Import-Module Ghost.Quoted"
    Write-Output 'function Get-QuotedGhost { }'
}
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "" {
		t.Errorf("imports = %q", got)
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "Get-Real" {
		t.Errorf("symbols = %q", got)
	}
}

// The two entrypoint signals, and the case that has neither. A param block is how a `.ps1`
// declares the arguments it is invoked with, and only a *top-level* one says so — a param
// block inside a function is that function's parameter list, which every well-written
// function has, so reading it as an entrypoint would mark every module in a repository as a
// command.
func TestPowerShellEntrypointsAreTopLevelOnly(t *testing.T) {
	script := extractPS(t, "a.ps1", `#!/usr/bin/env pwsh
param([string]$Name)

function Get-Thing {
    param([string]$Inner)
}
`)
	if got := strings.Join(script.Entrypoints, ","); got != "#!,param" {
		t.Errorf("entrypoints = %q, want both signals once", got)
	}
	module := extractPS(t, "M.psm1", `function Get-Thing {
    param([string]$Name)
}
`)
	if got := strings.Join(module.Entrypoints, ","); got != "" {
		t.Errorf("entrypoints = %q; a function's param block is not an entrypoint", got)
	}
}
