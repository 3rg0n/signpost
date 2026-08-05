package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func javaFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangJava, Class: discover.ClassSource, Content: src,
	}
}

func extractJava(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := JavaExtractor{}.Extract(javaFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real Java, including the forms a line
// matcher gets wrong here: a call in a method body that looks like a declaration, a
// field initialised with `new`, package-private members, and a text block holding
// what reads as source.
func javaCorpus() []Fixture {
	return []Fixture{
		{
			File: javaFile("src/main/java/com/example/api/Service.java", `
package com.example.api;

import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import static java.util.Collections.emptyList;
import com.example.store.Repository;
import com.example.store.*;

/**
 * Serves requests. The rest of the Javadoc is not the summary.
 *
 * @param nothing
 */
public class Service {
    private final Map<String, String> cache = new ConcurrentHashMap<>();
    private final Repository repo;

    public Service(Repository repo) {
        this.repo = repo;
    }

    /** Looks something up. */
    public List<String> lookup(String key) {
        if (cache.containsKey(key)) {
            return emptyList();
        }
        return repo.find(key);
    }

    void internalOnly() {
        helper(1, 2);
    }

    private int helper(int a, int b) {
        for (int i = 0; i < a; i++) {
            b += i;
        }
        return b;
    }

    public static void main(String[] args) {
        new Service(null).lookup("x");
    }
}
`),
			Expected: Expected{
				Package: "com.example.api",
				Imports: []string{
					"com.example.store", "java.util", "java.util.concurrent",
				},
				Symbols: []string{
					"Service", "Service.helper", "Service.internalOnly", "Service.lookup",
					"Service.main",
				},
				Exported:    []string{"Service", "Service.lookup", "Service.main"},
				Entrypoints: []string{"main"},
			},
		},
		{
			// An interface, whose members are public without saying so, and a
			// package-private class, whose public members are not public surface.
			File: javaFile("src/main/java/com/example/api/Contract.java", `
package com.example.api;

public interface Contract {
    String name();

    default boolean valid() {
        return name() != null;
    }

    interface Nested {
        void go();
    }
}

class Internal {
    public void looksPublic() {}
}

enum Mode {
    FAST,
    SLOW;

    public boolean isFast() {
        return this == FAST;
    }
}

record Point(int x, int y) {
    public int sum() {
        return x + y;
    }
}
`),
			Expected: Expected{
				Package: "com.example.api",
				Imports: []string{},
				Symbols: []string{
					"Contract", "Contract.name", "Contract.valid", "Internal",
					"Internal.looksPublic", "Mode", "Mode.isFast", "Nested", "Nested.go",
					"Point", "Point.sum",
				},
				// Mode, Point and Internal carry no modifier, so nothing they declare is
				// reachable outside the package. Contract's members need none.
				Exported:    []string{"Contract", "Contract.name", "Contract.valid", "Nested", "Nested.go"},
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial fixture: declarations inside strings, comments and a text
			// block, plus the statement forms that share a declaration's shape.
			File: javaFile("src/main/java/com/example/Tricky.java", `
package com.example;

import real.thing.Used;

public class Tricky {
    private static final String SNIPPET = "import fake.thing.Ghost; public class Phantom {}";

    private static final String BLOCK = """
        package nowhere;
        import nowhere.Nothing;
        public class TextBlockGhost {
            public void ghostMethod() {}
        }
        """;

    // import commented.Out;
    /* public class BlockGhost {} */

    public void real(String s) {
        char brace = '{';
        char quote = '"';
        if (s.isEmpty()) {
            System.out.println("not a declaration(x)");
        }
        while (s.length() > 0) {
            s = s.substring(1);
        }
        try {
            somethingElse(s);
        } catch (RuntimeException e) {
            throw new IllegalStateException(e);
        }
        Runnable r = () -> System.out.println(brace);
        r.run();
    }

    private void somethingElse(String s) {}
}
`),
			Expected: Expected{
				Package: "com.example",
				Imports: []string{"real.thing"},
				Symbols: []string{"Tricky", "Tricky.real", "Tricky.somethingElse"},
				Exported: []string{
					"Tricky", "Tricky.real",
				},
				Entrypoints: []string{},
			},
		},
		{
			// A wrapped declaration and a generic method: the two shapes that only read
			// correctly once the line is joined to where its parentheses balance.
			File: javaFile("src/main/java/com/example/Wrapped.java", `
package com.example;

import java.util.function.Function;

public abstract class Wrapped
        implements Function<String, String>,
                   Comparable<Wrapped> {

    public <T extends Comparable<T>> T pick(
            T first,
            T second) {
        return first.compareTo(second) >= 0 ? first : second;
    }

    @Override
    public String apply(
            String in) {
        return in;
    }

    protected abstract void hook();
}
`),
			Expected: Expected{
				Package: "com.example",
				Imports: []string{"java.util.function"},
				Symbols: []string{
					"Wrapped", "Wrapped.apply", "Wrapped.hook", "Wrapped.pick",
				},
				// protected is not public surface; the bundle's claim is what a caller
				// outside the package can reach.
				Exported:    []string{"Wrapped", "Wrapped.apply", "Wrapped.pick"},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises for Java.
func TestJavaExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(JavaExtractor{}, discover.LangJava, javaCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("Java extractor below target:\n%s", ls.Report())
	}
	t.Logf("Java extractor score:\n%s", ls.Report())
}

// The package declaration is the fact resolution runs on, because no build file is
// read (task #19 defers pom.xml and build.gradle) and the directory does not state
// it: the same file compiles from src/main/java, from src/, or from a Gradle source
// set that names any directory at all.
func TestJavaPackageComesFromTheDeclarationNotThePath(t *testing.T) {
	for _, path := range []string{
		"src/main/java/com/example/api/A.java",
		"src/com/example/api/A.java",
		"weird/place/A.java",
	} {
		fa := extractJava(t, path, "package com.example.api;\n\npublic class A {}\n")
		if fa.Package != "com.example.api" {
			t.Errorf("%s: Package = %q, want the declared name", path, fa.Package)
		}
	}
	// A file with no declaration is in the default package and claims no name, rather
	// than inheriting one from its directory.
	if fa := extractJava(t, "src/main/java/com/example/A.java", "public class A {}\n"); fa.Package != "" {
		t.Errorf("Package = %q for a file that declares none", fa.Package)
	}
}

// An import's dependency is the package, not the class. `com.example.util.Strings`
// as the raw path would point at a node no file declares, and two imports of
// different classes in one package would read as two dependencies.
func TestJavaImportSplitsPackageFromClass(t *testing.T) {
	fa := extractJava(t, "A.java", `
package p;

import com.example.util.Strings;
import com.example.util.Numbers;
import static com.example.util.Assert.isTrue;
import java.util.*;
import Toplevel;
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "com.example.util,java.util" {
		t.Errorf("ImportPaths = %q", got)
	}
	// The two classes are one dependency carrying both names, which is what makes the
	// page able to say which part of a package is used.
	for _, im := range fa.Imports {
		if im.Raw != "com.example.util" {
			continue
		}
		if got := strings.Join(im.Names, ","); got != "Assert,Numbers,Strings" {
			t.Errorf("names for com.example.util = %q", got)
		}
	}
}

// Java's default visibility is package-private, and reporting it as public would put
// every helper in the repository on the public-surface page. This is the rule Kotlin
// inverts, so it is asserted on its own rather than only through the corpus.
func TestJavaDefaultVisibilityIsPackagePrivate(t *testing.T) {
	fa := extractJava(t, "A.java", `
package p;

class Bare {
    void bare() {}
    public void stated() {}
}

public class Stated {
    void bare() {}
    public void stated() {}
    protected void guarded() {}
    private void hidden() {}
}
`)
	var exported []string
	for _, s := range fa.ExportedSymbols() {
		if s.Recv != "" {
			exported = append(exported, s.Recv+"."+s.Name)
			continue
		}
		exported = append(exported, s.Name)
	}
	if got := strings.Join(exported, ","); got != "Stated,Stated.stated" {
		t.Errorf("exported = %q; want only the public class and its public method", got)
	}
}

// A statement in a method body has the same shape as a declaration, and the depth
// rule is what tells them apart. Asserted independently of the corpus because a
// regression here inflates every file's symbol list rather than losing one entry.
func TestJavaDoesNotInventMethodsFromCalls(t *testing.T) {
	fa := extractJava(t, "A.java", `
package p;

public class A {
    private final java.util.Map<String, String> m = new java.util.HashMap<>();

    public void go() {
        compute(1);
        this.compute(2);
        A other = new A();
        other.compute(3);
        int n = compute(4);
        if (n > 0) { compute(5); }
        assert check(n);
        throw new IllegalStateException(describe(n));
    }

    private int compute(int x) { return x; }
    private boolean check(int x) { return x > 0; }
    private String describe(int x) { return ""; }
}
`)
	want := "A,A.check,A.compute,A.describe,A.go"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("SymbolNames = %q, want %q", got, want)
	}
}

// A constructor is not recorded, and the reason is what Symbol can hold: a name and
// a kind, no signature. `Service.Service` repeats the type's own name and adds
// nothing, while the parameters that distinguish two constructors are not a fact the
// record carries. An overloaded constructor would collapse to one entry anyway.
func TestJavaConstructorsAreNotSymbols(t *testing.T) {
	fa := extractJava(t, "A.java", `
package p;

public class Service {
    public Service() {}
    public Service(int n) {}
    public void work() {}
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Service,Service.work" {
		t.Errorf("SymbolNames = %q, want the type and its method only", got)
	}
}

// A nested type closing must restore the outer one. Getting this wrong files the
// rest of the outer class's methods under a type that has ended, which is a wrong
// owner rather than a missing symbol — the harder failure to notice on a page.
func TestJavaNestedTypesRestoreTheirOwner(t *testing.T) {
	fa := extractJava(t, "A.java", `
package p;

public class Outer {
    public void before() {}

    public static class Inner {
        public void inside() {}
    }

    public void after() {}
}
`)
	owners := map[string]string{}
	for _, s := range fa.Symbols {
		if s.Kind == SymMethod {
			owners[s.Name] = s.Recv
		}
	}
	for name, want := range map[string]string{
		"before": "Outer", "inside": "Inner", "after": "Outer",
	} {
		if owners[name] != want {
			t.Errorf("%s belongs to %q, want %q", name, owners[name], want)
		}
	}
}

// A char literal holding a brace is why the scanner keeps single quotes as a
// delimiter for Java: an unbalanced brace left in place opens a block that never
// closes, and every declaration after it reads as nested.
func TestJavaCharLiteralsDoNotSkewDepth(t *testing.T) {
	fa := extractJava(t, "A.java", `
package p;

public class A {
    public boolean isOpen(char c) {
        return c == '{';
    }

    public void after() {}
}
`)
	want := "A,A.after,A.isOpen"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("SymbolNames = %q, want %q — a char literal moved the brace depth", got, want)
	}
}

// A text block is a string, and a string holding source must contribute nothing.
// This is the dominant failure of a line-oriented extractor (CONTRIBUTING: inventing
// a declaration, not missing one), and Java's is the newest of the forms.
func TestJavaTextBlocksContributeNothing(t *testing.T) {
	fa := extractJava(t, "A.java", `
package p;

import real.Used;

public class A {
    static final String S = """
        package ghost;
        import ghost.Nothing;
        public class Ghost {
            public void ghostly() {}
        }
        """;

    public void real() {}
}
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "real" {
		t.Errorf("ImportPaths = %q, want just the real one", got)
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "A,A.real" {
		t.Errorf("SymbolNames = %q; the text block was read as code", got)
	}
}

// An entrypoint is the exact launcher signature. An instance method named main is an
// ordinary method, and reporting it as the program's start point sends a reader to a
// method nothing calls.
func TestJavaMainMustBeStatic(t *testing.T) {
	static := extractJava(t, "A.java", `
package p;
public class A {
    public static void main(String[] args) {}
}
`)
	if got := strings.Join(static.Entrypoints, ","); got != "main" {
		t.Errorf("Entrypoints = %q for the launcher signature", got)
	}
	instance := extractJava(t, "B.java", `
package p;
public class B {
    public void main(String[] args) {}
    public static void main() {}
}
`)
	if len(instance.Entrypoints) != 0 {
		t.Errorf("Entrypoints = %v; neither declaration is a launcher signature", instance.Entrypoints)
	}
}

// Javadoc is the only prose the deterministic pass can honestly attribute, and an
// annotation between the comment and the declaration is the normal formatting in any
// Spring or JUnit codebase.
func TestJavaDocSkipsAnnotationsAndTagBlocks(t *testing.T) {
	fa := extractJava(t, "A.java", `
package p;

/**
 * Does the thing.
 *
 * @param x the input
 * @return the output
 */
@Deprecated
@SuppressWarnings("unchecked")
public class A {
    /** Runs it. */
    @Override
    public void run() {}
}
`)
	docs := map[string]string{}
	for _, s := range fa.Symbols {
		docs[s.Name] = s.Doc
	}
	if docs["A"] != "Does the thing." {
		t.Errorf("A doc = %q", docs["A"])
	}
	if docs["run"] != "Runs it." {
		t.Errorf("run doc = %q", docs["run"])
	}
}
