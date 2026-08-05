package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func ktFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangKotlin, Class: discover.ClassSource, Content: src,
	}
}

func extractKotlin(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := KotlinExtractor{}.Extract(ktFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real Kotlin, including the forms that have
// no Java counterpart: top-level declarations with no owner, a primary constructor
// declaring properties, an aliased import, a companion object, and an extension
// function whose receiver is a type this file does not declare.
func kotlinCorpus() []Fixture {
	return []Fixture{
		{
			File: ktFile("src/main/kotlin/com/example/api/Service.kt", `
package com.example.api

import com.example.store.Repository
import kotlinx.coroutines.flow.Flow
import kotlin.math.max
import com.example.store.Entity as StoredEntity

/**
 * Serves requests.
 */
class Service(private val repo: Repository, val name: String) {

    /** Looks something up. */
    fun lookup(key: String): Flow<String> = repo.find(key)

    private fun helper(a: Int, b: Int): Int = max(a, b)

    internal fun moduleOnly() {}

    companion object {
        fun create(): Service = Service(Repository(), "default")
    }
}

fun main() {
    Service.create().lookup("x")
}

val DEFAULT_NAME = "service"

const val LIMIT = 100
`),
			Expected: Expected{
				Package: "com.example.api",
				Imports: []string{
					"com.example.store", "kotlin.math", "kotlinx.coroutines.flow",
				},
				Symbols: []string{
					"DEFAULT_NAME", "LIMIT", "Service", "Service.create", "Service.helper",
					"Service.lookup", "Service.moduleOnly", "Service.name", "Service.repo",
					"main",
				},
				// Public is the default, so what is missing here is what said otherwise:
				// `private val repo`, `private fun helper`, and `internal fun moduleOnly`.
				Exported: []string{
					"DEFAULT_NAME", "LIMIT", "Service", "Service.create", "Service.lookup",
					"Service.name", "main",
				},
				Entrypoints: []string{"main"},
			},
		},
		{
			// The declaration forms: interface, object, data class, enum class, sealed
			// class, typealias, and an extension function on a foreign type.
			File: ktFile("src/main/kotlin/com/example/api/Model.kt", `
package com.example.api

interface Contract {
    fun name(): String
    val id: Int
}

object Registry {
    private val entries = mutableMapOf<String, String>()
    fun put(k: String, v: String) { entries[k] = v }
}

data class Point(val x: Int, val y: Int)

enum class Mode {
    FAST,
    SLOW
}

sealed class Result {
    data class Ok(val value: String) : Result()
    object Empty : Result()
}

typealias Handler = (String) -> Unit

fun String.shout(): String = uppercase() + "!"

private fun internalHelper() {}
`),
			Expected: Expected{
				Package: "com.example.api",
				Imports: []string{},
				Symbols: []string{
					"Contract", "Contract.id", "Contract.name", "Empty", "Handler", "Mode",
					"Ok", "Ok.value", "Point", "Point.x", "Point.y", "Registry",
					"Registry.entries", "Registry.put", "Result", "internalHelper", "shout",
				},
				Exported: []string{
					"Contract", "Contract.id", "Contract.name", "Empty", "Handler", "Mode",
					"Ok", "Ok.value", "Point", "Point.x", "Point.y", "Registry",
					"Registry.put", "Result", "shout",
				},
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial fixture: declarations inside a raw string, a string
			// template, comments including the nested block comment Kotlin allows, and
			// statement forms that share a declaration's shape.
			File: ktFile("src/main/kotlin/com/example/Tricky.kt", `
package com.example

import real.thing.Used

class Tricky {
    private val snippet = "import fake.thing.Ghost; class Phantom"

    private val block = """
        package nowhere
        import nowhere.Nothing
        class RawGhost {
            fun ghostly() {}
        }
        """

    private val template = "value is ${'$'}{compute(1)}"

    // import commented.Out
    /* class BlockGhost */
    /* nested /* deeper */ still a comment
       class DeepGhost */

    fun real(s: String) {
        val brace = '{'
        compute(1)
        if (s.isEmpty()) {
            println("not a declaration(x)")
        }
        when (s) {
            "a" -> compute(2)
            else -> compute(3)
        }
        val local = fun(x: Int): Int = x
        fun localNamed() {}
        localNamed()
    }

    private fun compute(x: Int): Int = x
}
`),
			Expected: Expected{
				Package: "com.example",
				Imports: []string{"real.thing"},
				Symbols: []string{
					"Tricky", "Tricky.block", "Tricky.compute", "Tricky.real",
					"Tricky.snippet", "Tricky.template",
				},
				Exported:    []string{"Tricky", "Tricky.real"},
				Entrypoints: []string{},
			},
		},
		{
			// A .kts script: Kotlin with no package declaration and no enclosing type,
			// which is the shape a Gradle-adjacent script or a build helper takes.
			File: ktFile("scripts/report.kts", `
import java.io.File

val target = File("out")

fun render(name: String): String {
    return "report: ${'$'}name"
}

println(render(target.name))
`),
			Expected: Expected{
				Imports:     []string{"java.io"},
				Symbols:     []string{"render", "target"},
				Exported:    []string{"render", "target"},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises for Kotlin.
func TestKotlinExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(KotlinExtractor{}, discover.LangKotlin, kotlinCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("Kotlin extractor below target:\n%s", ls.Report())
	}
	t.Logf("Kotlin extractor score:\n%s", ls.Report())
}

// The rule the two JVM extractors must not share. Kotlin's default is public and
// Java's is package-private, so one shared isExported would invert the public surface
// of every file in whichever language lost. Both directions are asserted here against
// the same declarations, because a regression that copies one rule onto the other
// language passes each language's own corpus in only one direction.
func TestKotlinDefaultVisibilityIsPublicAndJavaIsNot(t *testing.T) {
	kt := extractKotlin(t, "A.kt", `
package p

class Bare {
    fun bare() {}
    private fun hidden() {}
    internal fun moduleOnly() {}
    protected fun guarded() {}
}
`)
	if got := strings.Join(exportedNames(kt), ","); got != "Bare,Bare.bare" {
		t.Errorf("Kotlin exported = %q; the unmodified declarations are the public ones", got)
	}

	// The same shape in Java: nothing is public, because none of it says so.
	jv := extractJava(t, "A.java", `
package p;

class Bare {
    void bare() {}
    private void hidden() {}
    protected void guarded() {}
}
`)
	if got := strings.Join(exportedNames(jv), ","); got != "" {
		t.Errorf("Java exported = %q; an unmodified declaration is package-private", got)
	}
}

// exportedNames renders the exported surface the way a fixture labels it.
func exportedNames(fa Facts) []string {
	var out []string
	for _, s := range fa.ExportedSymbols() {
		if s.Recv != "" {
			out = append(out, s.Recv+"."+s.Name)
			continue
		}
		out = append(out, s.Name)
	}
	return out
}

// `internal` is module-visible and is not the module's API, on the same grounds as
// Rust's `pub(crate)`. Asserted separately because it is the one Kotlin modifier with
// no Java counterpart, so nothing else in the suite covers it.
func TestKotlinInternalIsNotPublicSurface(t *testing.T) {
	fa := extractKotlin(t, "A.kt", `
package p

internal class ModuleOnly {
    fun reachableInside() {}
}

class Public {
    internal fun moduleOnly() {}
    fun open() {}
}
`)
	if got := strings.Join(exportedNames(fa), ","); got != "Public,Public.open" {
		t.Errorf("exported = %q; internal is not the module's public API", got)
	}
}

// A top-level declaration has no owner, which is ordinary Kotlin and impossible in
// Java. An empty scope stack is a valid state, not a parse failure, and treating it as
// one would drop every declaration in a file that declares no class.
func TestKotlinTopLevelDeclarationsNeedNoType(t *testing.T) {
	fa := extractKotlin(t, "util.kt", `
package p

fun helper(): Int = 1
val constant = 2
var mutable = 3
typealias Alias = String
`)
	want := "Alias,constant,helper,mutable"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("SymbolNames = %q, want %q", got, want)
	}
	for _, s := range fa.Symbols {
		if s.Recv != "" {
			t.Errorf("%s was given owner %q; a top-level declaration has none", s.Name, s.Recv)
		}
	}
}

// A primary constructor is where a data class states everything it holds. Reading only
// the class name from `data class Point(val x: Int, val y: Int)` would put a type on
// the page with no surface at all.
func TestKotlinPrimaryConstructorDeclaresProperties(t *testing.T) {
	fa := extractKotlin(t, "A.kt", `
package p

data class Point(val x: Int, val y: Int = 0, private val hidden: String = f(1))

class Logger(name: String) {
    val prefix = name
}
`)
	want := "Logger,Logger.prefix,Point,Point.hidden,Point.x,Point.y"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("SymbolNames = %q, want %q", got, want)
	}
	// A plain parameter declares no property, so `name` is not surface.
	for _, s := range fa.Symbols {
		if s.Name == "name" {
			t.Error("a constructor parameter with no val/var was recorded as a property")
		}
	}
	// Ordered by symbol name, which is the order Normalize leaves: a method sorts by
	// its own name, not under its owner.
	if got := strings.Join(exportedNames(fa), ","); got != "Logger,Point,Logger.prefix,Point.x,Point.y" {
		t.Errorf("exported = %q; the private property is not surface", got)
	}
}

// A companion object's members are called through the enclosing type, so that is the
// owner they get. Attributing them to a type named "Companion" would put a page
// heading on something no caller names.
func TestKotlinCompanionMembersBelongToTheEnclosingType(t *testing.T) {
	fa := extractKotlin(t, "A.kt", `
package p

class Service {
    fun instance() {}

    companion object {
        fun create(): Service = Service()
        const val DEFAULT = "x"
    }

    fun after() {}
}
`)
	owners := map[string]string{}
	for _, s := range fa.Symbols {
		owners[s.Name] = s.Recv
	}
	for name, want := range map[string]string{
		"instance": "Service", "create": "Service", "DEFAULT": "Service", "after": "Service",
	} {
		if owners[name] != want {
			t.Errorf("%s belongs to %q, want %q", name, owners[name], want)
		}
	}
	// A *named* companion does declare a name, and that name is what a caller uses.
	named := extractKotlin(t, "B.kt", `
package p

class Service {
    companion object Factory {
        fun create(): Service = Service()
    }
}
`)
	for _, s := range named.Symbols {
		if s.Name == "create" && s.Recv != "Factory" {
			t.Errorf("create belongs to %q, want Factory", s.Recv)
		}
	}
}

// An extension function's receiver is a type this file does not declare. Recording
// `String.shout` as a method of String would put a member on a page for a class the
// repository does not contain.
func TestKotlinExtensionFunctionsHaveNoOwnerHere(t *testing.T) {
	fa := extractKotlin(t, "ext.kt", `
package p

fun String.shout(): String = uppercase()
fun List<Int>.total(): Int = sum()
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "shout,total" {
		t.Errorf("SymbolNames = %q", got)
	}
	for _, s := range fa.Symbols {
		if s.Recv != "" {
			t.Errorf("%s was attributed to %q, a type this file does not declare", s.Name, s.Recv)
		}
	}
}

// Kotlin's block comments nest, which Java's do not. A scanner that stops at the first
// `*/` reads the tail of a nested comment as code, and the declarations in it are
// invented rather than missed.
func TestKotlinNestedBlockCommentsStayComments(t *testing.T) {
	fa := extractKotlin(t, "A.kt", `
package p

/* outer /* inner */
   class GhostInComment
   fun ghostly() {}
*/

class Real {
    fun work() {}
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Real,Real.work" {
		t.Errorf("SymbolNames = %q; a nested comment was read as code", got)
	}
}

// A raw string holding source contributes nothing, and neither does a string template.
// Kotlin's `"""` is a raw string where Java's is a text block, and a template's `${...}`
// can hold a call — none of it is a declaration.
func TestKotlinRawStringsAndTemplatesContributeNothing(t *testing.T) {
	fa := extractKotlin(t, "A.kt", `
package p

import real.Used

val script = """
    package ghost
    import ghost.Nothing
    class Ghost {
        fun ghostly() {}
    }
    """

fun real(): String = "value: ${'$'}{compute()}"

private fun compute(): Int = 1
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "real" {
		t.Errorf("ImportPaths = %q, want just the real one", got)
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "compute,real,script" {
		t.Errorf("SymbolNames = %q; the raw string was read as code", got)
	}
}

// An aliased import is Kotlin's only JVM rename, and a file importing two same-named
// classes needs it. The dependency is still the package.
func TestKotlinAliasedImportKeepsTheAlias(t *testing.T) {
	fa := extractKotlin(t, "A.kt", `
package p

import com.example.store.Entity as StoredEntity
import com.other.wire.Entity as WireEntity
import kotlin.collections.*
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "com.example.store,com.other.wire,kotlin.collections" {
		t.Errorf("ImportPaths = %q", got)
	}
	aliases := map[string]string{}
	for _, im := range fa.Imports {
		aliases[im.Raw] = im.Alias
	}
	if aliases["com.example.store"] != "StoredEntity" || aliases["com.other.wire"] != "WireEntity" {
		t.Errorf("aliases = %v", aliases)
	}
}

// `fun main()` at the top level is where a Kotlin program starts, and unlike Java it
// needs no parameter. A main declared inside a type is a member, not an entrypoint.
func TestKotlinMainIsTopLevel(t *testing.T) {
	top := extractKotlin(t, "Main.kt", "package p\n\nfun main() {}\n")
	if got := strings.Join(top.Entrypoints, ","); got != "main" {
		t.Errorf("Entrypoints = %q for a top-level main", got)
	}
	member := extractKotlin(t, "A.kt", `
package p

class A {
    fun main() {}
}
`)
	if len(member.Entrypoints) != 0 {
		t.Errorf("Entrypoints = %v; a member named main is not the program's start", member.Entrypoints)
	}
}

// A local declaration inside a function body is unreachable from outside and is not
// surface. Kotlin permits a named local fun, which is the shape that makes this more
// than the depth rule Java needs.
func TestKotlinLocalDeclarationsAreNotSurface(t *testing.T) {
	fa := extractKotlin(t, "A.kt", `
package p

fun outer() {
    fun inner() {}
    val localVal = 1
    class LocalClass
    inner()
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "outer" {
		t.Errorf("SymbolNames = %q; only the top-level fun is surface", got)
	}
}
