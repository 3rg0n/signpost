package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func cFile(path, src string, lang discover.Lang) discover.File {
	return discover.File{
		Path: path, Lang: lang, Class: discover.ClassSource, Content: src,
	}
}

func extractC(t *testing.T, path, src string, lang discover.Lang) Facts {
	t.Helper()
	fa, err := CExtractor{}.Extract(cFile(path, src, lang))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real C, C++ and Objective-C, and weighted
// toward the forms a line matcher gets wrong: a call statement shaped like a
// declaration, a forward declaration that defines nothing, a macro invocation at file
// scope, a class whose members are private until a `public:` says otherwise, and an
// include written inside a string literal.
func cCorpus() []Fixture {
	return []Fixture{
		{
			File: cFile("src/buffer.c", `
#include <stdio.h>
#include <stdlib.h>
#include "buffer.h"
#include "util/log.h"

/** Grows the buffer. The rest of the Doxygen is not the summary.
 *
 * @param b the buffer
 */
int buffer_grow(Buffer *b, size_t want) {
    if (want < b->cap) {
        return 0;
    }
    void *p = realloc(b->data, want);
    if (!p) {
        log_error("out of memory");
        return -1;
    }
    b->data = p;
    b->cap = want;
    return 0;
}

static void buffer_reset(Buffer *b) {
    buffer_grow(b, 0);
}

int main(int argc, char **argv) {
    Buffer b = {0};
    buffer_grow(&b, 16);
    return 0;
}
`, discover.LangC),
			Expected: Expected{
				Imports: []string{
					`"buffer.h"`, `"util/log.h"`, "<stdio.h>", "<stdlib.h>",
				},
				Symbols:     []string{"buffer_grow", "buffer_reset", "main"},
				Exported:    []string{"buffer_grow", "main"},
				Entrypoints: []string{"main"},
			},
		},
		{
			// A header: prototypes and a struct definition are the whole surface, and a
			// forward declaration defines nothing. The include guard must contribute no
			// symbol and no depth.
			File: cFile("src/buffer.h", `
#ifndef BUFFER_H
#define BUFFER_H

#include <stddef.h>

struct Arena;

struct Buffer {
    char *data;
    size_t cap;
};

typedef struct Buffer Buffer;

int buffer_grow(Buffer *b, size_t want);
void buffer_free(Buffer *b);

#define BUFFER_MIN(a, b) ((a) < (b) ? (a) : (b))

#endif
`, discover.LangC),
			Expected: Expected{
				Imports:     []string{"<stddef.h>"},
				Symbols:     []string{"Buffer", "buffer_free", "buffer_grow"},
				Exported:    []string{"Buffer", "buffer_free", "buffer_grow"},
				Entrypoints: []string{},
			},
		},
		{
			File: cFile("src/session.cc", `
#include <memory>
#include <string>
#include <vector>
#include "session.hpp"

namespace net {
namespace {

int internal_counter() {
    return 0;
}

}  // namespace

/// A live connection.
class Session {
public:
    Session(std::string host);
    ~Session();

    bool open();
    void close();

private:
    bool handshake();

    std::string host_;
    std::vector<std::string> log_;
};

bool Session::open() {
    if (handshake()) {
        return true;
    }
    return false;
}

bool Session::handshake() {
    return true;
}

}  // namespace net
`, discover.LangCpp),
			Expected: Expected{
				Imports: []string{
					`"session.hpp"`, "<memory>", "<string>", "<vector>",
				},
				Symbols: []string{
					"Session", "Session.close", "Session.handshake", "Session.open",
					"internal_counter",
				},
				Exported:    []string{"Session", "Session.close", "Session.open"},
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial file. Every line below either looks like a declaration and
			// is not one, or is a declaration a matcher would skip. Nothing here may
			// yield a symbol except the two functions and the one struct.
			File: cFile("src/tricky.c", `
#include "real.h"

/*
 * #include "commented_out.h"
 * int commented_function(void);
 */

// #include "line_commented.h"

static const char *SNIPPET =
    "#include \"in_a_string.h\"\n"
    "int string_function(void) { return 0; }\n";

MODULE_LICENSE("GPL");
DEFINE_MUTEX(the_lock);

struct Pending;

int (*dispatch_table)(int) = NULL;

typedef int (*handler_fn)(void *);

struct Real {
    int x;
};

struct Real *real_make(size_t cap) {
    return NULL;
}

union Slot *slot_of(int tag);

extern struct Registry g_registry;

/* An attribute carries a parenthesised list, and every rule here is written against the
 * first parenthesis being the parameter list. In front of the return type it moves that
 * parenthesis and takes the declaration with it. */
__attribute__((unused)) static int real_unused(void) { return 0; }

__declspec(dllexport) int real_exported(void) { return 0; }

/* An attribute between the keyword and the name has the same effect on a type. */
struct __attribute__((packed)) RealPacked {
    int x;
};

int real_work(int n) {
    int total = 0;
    for (int i = 0; i < n; i++) {
        total += helper_call(i);
    }
    if (total > 10) {
        return clamp(total, 10);
    }
    while (total) {
        total--;
    }
    return total;
}

void also_real(void) {
    char c = '{';
    char d = '}';
    real_work(1);
}
`, discover.LangC),
			Expected: Expected{
				Imports: []string{`"real.h"`},
				Symbols: []string{
					"Real", "RealPacked", "also_real", "real_exported", "real_make",
					"real_unused", "real_work", "slot_of",
				},
				Exported: []string{
					"Real", "RealPacked", "also_real", "real_exported", "real_make",
					"real_work", "slot_of",
				},
				Entrypoints: []string{},
			},
		},
		{
			File: cFile("src/Reader.m", `
#import <Foundation/Foundation.h>
#import "Reader.h"

@interface Reader ()
- (BOOL)validate;
@end

@implementation Reader

- (instancetype)initWithPath:(NSString *)path {
    self = [super init];
    return self;
}

- (NSString *)readLine {
    [self validate];
    return @"";
}

- (void)setName:(NSString *)name age:(int)age {
    _name = name;
}

+ (Reader *)readerWithPath:(NSString *)path {
    return [[Reader alloc] initWithPath:path];
}

- (BOOL)validate {
    return YES;
}

@end
`, discover.LangObjC),
			Expected: Expected{
				Imports: []string{
					`"Reader.h"`, "<Foundation/Foundation.h>",
				},
				Symbols: []string{
					"Reader.initWithPath:", "Reader.readLine", "Reader.readerWithPath:",
					"Reader.setName:age:", "Reader.validate",
				},
				Entrypoints: []string{},
			},
		},
		{
			File: cFile("src/Reader.h", `
#import <Foundation/Foundation.h>

@protocol Readable <NSObject>
- (NSString *)readLine;
@end

@interface Reader : NSObject <Readable>
@property (nonatomic, copy) NSString *name;
- (instancetype)initWithPath:(NSString *)path;
+ (Reader *)readerWithPath:(NSString *)path;
@end

@interface NSString (ReaderExtras)
- (BOOL)looksLikePath;
@end
`, discover.LangObjC),
			Expected: Expected{
				Imports: []string{"<Foundation/Foundation.h>"},
				Symbols: []string{
					"NSString.looksLikePath", "Readable", "Readable.readLine", "Reader",
					"Reader.initWithPath:", "Reader.readerWithPath:",
				},
				Entrypoints: []string{},
			},
		},
	}
}

func TestCExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(CExtractor{}, discover.LangC, cCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("C extractor below target:\n%s", ls.Report())
	}
	t.Log("\n" + ls.Report())
}

func TestCExtractorClaimsTheWholeFamily(t *testing.T) {
	langs := CExtractor{}.Langs()
	want := map[discover.Lang]bool{
		discover.LangC: false, discover.LangCpp: false, discover.LangObjC: false,
	}
	for _, l := range langs {
		if _, ok := want[l]; !ok {
			t.Errorf("Langs() includes unexpected %q", l)
			continue
		}
		want[l] = true
	}
	for l, seen := range want {
		if !seen {
			t.Errorf("Langs() missing %q — files of that language reach no extractor", l)
		}
	}
}

func TestCFactsKeepTheDispatchedLanguage(t *testing.T) {
	// A .h classified as C may hold C++, and the extractor reads it either way. What
	// it must not do is relabel the file: discovery owns Lang, and an extractor that
	// overrode it would disagree with the census the same run reports.
	fa := extractC(t, "src/widget.h", "class Widget {\npublic:\n  void draw();\n};\n",
		discover.LangC)
	if fa.Lang != discover.LangC {
		t.Errorf("Lang = %q, should follow the input file", fa.Lang)
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "Widget,Widget.draw" {
		t.Errorf("symbols = %q, a C++ class in a .h should still be read", got)
	}
}

func TestCIncludeKeepsItsDelimiters(t *testing.T) {
	// The delimiter is the resolution rule and nothing else on the line carries it.
	// Dropping it makes every include look repo-relative.
	fa := extractC(t, "a.c", "#include <stdio.h>\n#include \"local.h\"\n", discover.LangC)
	got := strings.Join(fa.ImportPaths(), ",")
	if got != `"local.h",<stdio.h>` {
		t.Errorf("imports = %q, want the delimiters preserved", got)
	}
}

func TestCIncludeFormsAreRead(t *testing.T) {
	cases := map[string]string{
		"#include <stdio.h>":           "<stdio.h>",
		"#include \"a/b.h\"":           `"a/b.h"`,
		"#  include   <sys/types.h>":   "<sys/types.h>",
		"#import <Foundation/NSURL.h>": "<Foundation/NSURL.h>",
		"#include_next <limits.h>":     "<limits.h>",
		"#include\t\"tabbed.h\"":       `"tabbed.h"`,
	}
	for src, want := range cases {
		fa := extractC(t, "a.c", src+"\n", discover.LangC)
		got := strings.Join(fa.ImportPaths(), ",")
		if got != want {
			t.Errorf("%q -> %q, want %q", src, got, want)
		}
	}
}

func TestCIncludeRejectsWhatNamesNoFile(t *testing.T) {
	// A macro-valued include names no file this extractor can know, and recording the
	// macro's name as a path would point at something that does not exist. The other
	// rows are directives that are not includes at all.
	for _, src := range []string{
		"#include HEADER_NAME",
		"#include",
		"#include <>",
		`#include ""`,
		"#define include <stdio.h>",
		"#pragma once",
		"#if defined(__linux__)",
	} {
		fa := extractC(t, "a.c", src+"\n", discover.LangC)
		if len(fa.Imports) != 0 {
			t.Errorf("%q yielded %v, want no import", src, fa.ImportPaths())
		}
	}
}

func TestCStaticIsNotPublicSurface(t *testing.T) {
	// C inverts Java's default: absence of a keyword means external linkage, and
	// `static` is the one keyword that removes a symbol from the link surface.
	src := `
int external_fn(void) { return 0; }
static int internal_fn(void) { return 1; }
static_assert(1, "not a declaration");
int static_looking_name(void) { return 2; }
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "external_fn,internal_fn,static_looking_name" {
		t.Errorf("symbols = %q", got)
	}
	var exported []string
	for _, s := range fa.ExportedSymbols() {
		exported = append(exported, s.Name)
	}
	if got := strings.Join(exported, ","); got != "external_fn,static_looking_name" {
		t.Errorf("exported = %q, want static_fn excluded and the name containing "+
			"'static' kept", got)
	}
}

func TestCDoesNotInventFunctionsFromCalls(t *testing.T) {
	// The failure mode this extractor exists to avoid. Every line in the body has a
	// call's shape, which is a declaration's shape minus the return type.
	src := `
void caller(void) {
    doThing(1, 2);
    if (check(3)) {
        other(4);
    }
    while (more(5)) {
        yet_another(6);
    }
    int *p = malloc(7);
    return;
}
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "caller" {
		t.Errorf("symbols = %q, want only the declaration", got)
	}
}

func TestCMacroInvocationIsNotADeclaration(t *testing.T) {
	// A file-scope macro call is idiomatic in kernel and test code, and it has a
	// declaration's exact shape at a declaration's exact depth. Only the return type
	// in front of the name tells them apart.
	src := `
MODULE_LICENSE("GPL");
EXPORT_SYMBOL(real_fn);
TEST(SuiteName, CaseName) {
    ASSERT_EQ(1, 1);
}
int real_fn(void) { return 0; }
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "real_fn" {
		t.Errorf("symbols = %q, want only real_fn", got)
	}
}

func TestCForwardDeclarationDefinesNothing(t *testing.T) {
	// A forward declaration promises a type exists so a pointer to it can be declared.
	// Recording one claims a type is defined in a file that deliberately does not
	// define it — and pushing a scope for one would claim every later declaration as
	// its member, which is what the second half of this test checks.
	src := `
struct Pending;
class Widget;
union Slot;

struct Defined {
    int x;
};

int after(void) { return 0; }
`
	fa := extractC(t, "a.h", src, discover.LangC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Defined,after" {
		t.Errorf("symbols = %q, want the forward declarations excluded and `after` "+
			"attributed to no type", got)
	}
	for _, s := range fa.Symbols {
		if s.Recv != "" {
			t.Errorf("%s has receiver %q; a forward declaration must open no scope",
				s.Name, s.Recv)
		}
	}
}

func TestCStructReturningFunctionIsNotATypeDefinition(t *testing.T) {
	// `struct Buffer *buffer_make(size_t)` returns a pointer to a struct. Its body's
	// brace is on the same line as the type keyword and the type's name, so a brace is
	// not enough to say a definition follows: reading one here reported a phantom
	// `Buffer` defined in a file that only mentions it, opened a scope that claimed the
	// rest of the file, and lost `buffer_make` — the one symbol the line declares.
	//
	// The rule is what sits between the name and the brace. A definition allows only
	// `final` and a `:` clause there; a declarator puts its own name there.
	src := `
struct Buffer *buffer_make(size_t cap) {
    return NULL;
}

union Value *value_of(int tag) {
    return NULL;
}

enum Mode *mode_of(int i);

int after(void) { return 0; }
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "after,buffer_make,mode_of,value_of" {
		t.Errorf("symbols = %q, want the four functions and no phantom type", got)
	}
	for _, s := range fa.Symbols {
		if s.Recv != "" {
			t.Errorf("%s has receiver %q; a struct-returning function opens no type scope",
				s.Name, s.Recv)
		}
	}
}

func TestCppTypeBodyIsFoundBelowAWrappedHead(t *testing.T) {
	// C++ style puts the opening brace below a wrapped head, and a base-class list is
	// conventionally one base per line. The lookahead has to be wide enough for a real
	// one: bounded at five lines, a class with six bases yielded no symbol at all and
	// appeared nowhere in the bundle. The guard against a forward declaration reaching a
	// later type's brace is the semicolon that ends it, not the width of the window,
	// which is why TestCForwardDeclarationDefinesNothing still holds.
	src := `
class Widget
    : public Base1,
      public Base2,
      public Base3,
      public Base4,
      public Base5,
      public Base6
{
public:
    void draw();
};

class Sealed final
    : public Base1
{
public:
    void go();
};

enum class Mode
    : unsigned int
{
    Fast,
};
`
	fa := extractC(t, "w.hpp", src, discover.LangCpp)
	want := "Mode,Sealed,Sealed.go,Widget,Widget.draw"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q", got, want)
	}
}

func TestCAttributesAndExportMacrosDoNotEatTheName(t *testing.T) {
	// An attribute carries a parenthesised argument list, and every rule in cFuncDecl is
	// written against the first parenthesis on the line being the *parameter* list. So an
	// attribute in front of the return type moves that parenthesis and takes the whole
	// declaration with it. Both spellings are ordinary in real C: the GNU form guards half
	// of any portable header, and `__declspec(dllexport)` is how a symbol leaves a DLL.
	//
	// An export macro has no parenthesis and fails differently — it simply sits where the
	// type's name is expected, so the type gets named after the macro or after nothing.
	cases := []struct {
		name, src, want string
	}{
		{"gnu attribute before a function", `__attribute__((unused)) static int helper(void) { return 0; }`, "helper"},
		{"msvc declspec before a function", `__declspec(dllexport) int exported(void) { return 0; }`, "exported"},
		{"c++11 attribute before a function", `[[nodiscard]] int size(void);`, "size"},
		{"attribute before a struct-returning function", `struct __attribute__((packed)) Buffer *make(void) { return NULL; }`, "make"},
		{"attribute between keyword and name", `struct __attribute__((packed)) Packed { int x; };`, "Packed"},
		{"declspec between keyword and name", `class __declspec(dllexport) Exported { void go(); };`, "Exported"},
		{"alignment specifier between keyword and name", `struct alignas(16) Aligned { int x; };`, "Aligned"},
		{"export macro between keyword and name", `class CORPUS_API Session { void open(); };`, "Session"},
		// A type whose own name shouts, which Win32 headers do constantly. The macro skip
		// must not consume the name when the name is all it has.
		{"a shouting type name is still the name", `struct POINT { int x; };`, "POINT"},
		{"a macro and a shouting name together", `struct CORPUS_API POINT { int x; };`, "POINT"},
		// A `:` clause is not part of the head, and cutting there is what keeps the search
		// for a name out of it. Read past the colon, a shouting type with a base list is
		// named after its base, and a scoped enum after its underlying type.
		{"a shouting name with a base list", `struct POINT : BASE { int x; };`, "POINT"},
		{"a shouting scoped enum with an underlying type", `enum class MODE : unsigned int { Fast };`, "MODE"},
	}
	for _, c := range cases {
		fa := extractC(t, "x.hpp", "\n"+c.src+"\n", discover.LangCpp)
		var got []string
		for _, s := range fa.Symbols {
			if s.Recv == "" {
				got = append(got, s.Name)
			}
		}
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%s: top-level symbols = %v, want exactly [%s]", c.name, got, c.want)
		}
	}
}

func TestCStructVariableIsNotATypeDeclaration(t *testing.T) {
	// `struct Buffer buf;` shares its first two tokens with a definition and declares
	// no type at all.
	src := `
struct Buffer buf;
static struct Buffer cached;
struct Buffer *ptr = NULL;
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if len(fa.Symbols) != 0 {
		t.Errorf("symbols = %v, want none", fa.SymbolNames())
	}
}

func TestCppClassMembersArePrivateUntilPublic(t *testing.T) {
	// The one place visibility is a property of position in the body rather than of the
	// declaration's own line. Without the access-specifier switch, a class reports its
	// whole surface as unreachable.
	src := `
class Widget {
    void hidden();

public:
    void shown();
    int also_shown();

private:
    void hidden_again();

protected:
    void guarded();
};
`
	fa := extractC(t, "a.hpp", src, discover.LangCpp)
	var exported []string
	for _, s := range fa.ExportedSymbols() {
		exported = append(exported, s.Name)
	}
	if got := strings.Join(exported, ","); got != "Widget,also_shown,shown" {
		t.Errorf("exported = %q", got)
	}
}

func TestCppStructMembersArePublicByDefault(t *testing.T) {
	// A struct and a class differ in exactly one way and this is it. Getting it
	// backwards reports a struct's whole interface as private.
	src := `
struct Point {
    int sum();
    void scale(int by);
};
`
	fa := extractC(t, "a.hpp", src, discover.LangCpp)
	if len(fa.ExportedSymbols()) != 3 {
		t.Errorf("exported = %v, want the type and both members", fa.SymbolNames())
	}
}

func TestCppAnonymousNamespaceIsNotSurface(t *testing.T) {
	// An anonymous namespace is `static` spelled as a scope: everything inside has
	// internal linkage.
	src := `
namespace {
int hidden(void) { return 0; }
}

namespace util {
int shown(void) { return 1; }
}
`
	fa := extractC(t, "a.cc", src, discover.LangCpp)
	if got := strings.Join(fa.SymbolNames(), ","); got != "hidden,shown" {
		t.Errorf("symbols = %q", got)
	}
	var exported []string
	for _, s := range fa.ExportedSymbols() {
		exported = append(exported, s.Name)
	}
	if got := strings.Join(exported, ","); got != "shown" {
		t.Errorf("exported = %q, want only the named namespace's member", got)
	}
}

func TestCppNamespaceDoesNotOwnItsFunctions(t *testing.T) {
	// A namespace qualifies a name without owning it, so a function in one is a
	// function and not a method. A namespace alias must open no scope at all.
	src := `
namespace alias = other::deep::ns;

namespace util {
int helper(void) { return 0; }
}

int after(void) { return 1; }
`
	fa := extractC(t, "a.cc", src, discover.LangCpp)
	for _, s := range fa.Symbols {
		if s.Recv != "" {
			t.Errorf("%s has receiver %q, want none", s.Name, s.Recv)
		}
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "after,helper" {
		t.Errorf("symbols = %q", got)
	}
}

func TestCppQualifiedDefinitionRestoresItsOwner(t *testing.T) {
	// An out-of-line member definition names its own owner, which is the only place
	// the receiver can come from when the class body is in another file.
	src := `
bool Session::open() { return true; }
void net::Session::close() {}
int free_function() { return 0; }
`
	fa := extractC(t, "a.cc", src, discover.LangCpp)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Session.close,Session.open,free_function" {
		t.Errorf("symbols = %q, want the last qualifier as the receiver", got)
	}
}

func TestCFunctionPointerIsNotAFunction(t *testing.T) {
	// A function pointer is data. Its declarator wraps the name in parentheses, which
	// puts a name where a function's would be.
	src := `
int (*dispatch)(int) = NULL;
static void (*hook)(void);
typedef int (*handler_fn)(void *);
int real(int x) { return x; }
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "real" {
		t.Errorf("symbols = %q, want only the real function", got)
	}
}

func TestCCharLiteralsDoNotSkewDepth(t *testing.T) {
	// The scanner blanks a delimited body, so a braced char literal contributes no
	// brace to the depth count. Reading `'{'` as code would open a block that never
	// closes and swallow every later declaration.
	src := `
void first(void) {
    char open = '{';
    char close = '}';
}

void second(void) {
    char quote = '"';
}

int third(void) { return 0; }
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "first,second,third" {
		t.Errorf("symbols = %q, want all three at file scope", got)
	}
}

func TestCPreprocessorBracesDoNotSkewDepth(t *testing.T) {
	// A directive's braces are not code braces. `#define BLOCK {` would otherwise open
	// a depth that never closes, and every later declaration would sit too deep for
	// cDeclSite to accept.
	src := `
#define OPEN_BLOCK {
#define CLOSE_BLOCK }
#define WRAP(x) do { x; } while (0)

int after_macros(void) { return 0; }
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "after_macros" {
		t.Errorf("symbols = %q", got)
	}
}

func TestCMacroGuardedCodeStillContributes(t *testing.T) {
	// Which branch a build selects is a fact about the build, not about the file. A
	// reader of the bundle needs both, and signpost reads no compiler flags to choose.
	src := `
#ifdef _WIN32
int platform_open(const char *p) { return 0; }
#else
int platform_open(const char *p) { return 1; }
#endif

#if 0
int never_compiled(void) { return 2; }
#endif
`
	fa := extractC(t, "a.c", src, discover.LangC)
	// Both arms declare the same name, which Normalize dedupes; the `#if 0` arm is
	// still a declaration in the file and is reported.
	got := strings.Join(fa.SymbolNames(), ",")
	if !strings.Contains(got, "platform_open") || !strings.Contains(got, "never_compiled") {
		t.Errorf("symbols = %q, want both guarded declarations", got)
	}
}

func TestCBlockCommentsDoNotNest(t *testing.T) {
	// The C standard says the first `*/` closes. Treating the comment as nesting would
	// swallow the rest of the file.
	src := `
/* outer /* inner */
int after_comment(void) { return 0; }
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "after_comment" {
		t.Errorf("symbols = %q, want the declaration after the closed comment", got)
	}
}

func TestCDeclarationsInCommentsAndStringsContributeNothing(t *testing.T) {
	src := `
// #include "commented.h"
// int commented_fn(void);

/*
#include "blocked.h"
int blocked_fn(void);
*/

static const char *SRC = "#include \"stringed.h\"";

int real_fn(void) { return 0; }
`
	fa := extractC(t, "a.c", src, discover.LangC)
	if len(fa.Imports) != 0 {
		t.Errorf("imports = %v, want none", fa.ImportPaths())
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "SRC,real_fn" && got != "real_fn" {
		t.Errorf("symbols = %q, want no symbol from a comment or a string", got)
	}
}

func TestCMainIsAnEntrypointOnlyAtFileScope(t *testing.T) {
	// C's main takes no modifier and any of several signatures, so the name at file
	// scope is the whole test — which makes the negative case the one worth stating: a
	// method named main is not where a program starts.
	fa := extractC(t, "a.c", "int main(void) { return 0; }\n", discover.LangC)
	if got := strings.Join(fa.Entrypoints, ","); got != "main" {
		t.Errorf("entrypoints = %q, want main", got)
	}

	fa = extractC(t, "a.cc", "class App {\npublic:\n  int main();\n};\n", discover.LangCpp)
	if len(fa.Entrypoints) != 0 {
		t.Errorf("entrypoints = %v, a member named main is not an entrypoint",
			fa.Entrypoints)
	}

	fa = extractC(t, "a.c", "void run(void) {\n  main();\n}\n", discover.LangC)
	if len(fa.Entrypoints) != 0 {
		t.Errorf("entrypoints = %v, a call to main is not a declaration of it",
			fa.Entrypoints)
	}
}

func TestObjCSelectorKeepsEveryPart(t *testing.T) {
	// A selector is interleaved with the parameters, and recording only its first part
	// would collapse `setName:` and `setName:age:` into one name. They are different
	// methods.
	src := `
@interface Person : NSObject
- (void)setName:(NSString *)n;
- (void)setName:(NSString *)n age:(int)a;
- (NSString *)describe;
+ (instancetype)personWithName:(NSString *)n age:(int)a city:(NSString *)c;
@end
`
	fa := extractC(t, "a.h", src, discover.LangObjC)
	want := "Person,Person.describe,Person.personWithName:age:city:,Person.setName:," +
		"Person.setName:age:"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q,\nwant %q", got, want)
	}
}

func TestObjCCategoryAddsToItsSubjectWithoutDeclaringIt(t *testing.T) {
	// A category reopens a type declared elsewhere. Its methods belong to that type;
	// the category itself is not a new type, and recording one would put a type on the
	// page that this file does not define.
	src := `
@interface NSString (Extras)
- (BOOL)looksLikePath;
@end
`
	fa := extractC(t, "a.h", src, discover.LangObjC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "NSString.looksLikePath" {
		t.Errorf("symbols = %q, want the method attributed to NSString and no type", got)
	}
}

func TestObjCEndClosesTheInterface(t *testing.T) {
	// `@end` closes the scope, not a brace. A method after it belongs to no type, and
	// an interface left open would claim every later method as its own.
	src := `
@interface First : NSObject
- (void)one;
@end

@interface Second : NSObject
- (void)two;
@end
`
	fa := extractC(t, "a.h", src, discover.LangObjC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "First,First.one,Second,Second.two" {
		t.Errorf("symbols = %q", got)
	}
}

func TestObjCMessageSendIsNotAMethodDeclaration(t *testing.T) {
	// A message send inside an implementation is the shape a declaration has to be told
	// apart from, and the leading sign is what distinguishes them.
	src := `
@implementation Reader
- (void)go {
    [self validate];
    [[Reader alloc] init];
    NSString *s = [NSString stringWithFormat:@"%d", 1];
}
@end
`
	fa := extractC(t, "a.m", src, discover.LangObjC)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Reader.go" {
		t.Errorf("symbols = %q, want only the declared method", got)
	}
}

func TestObjCProtocolIsAnInterface(t *testing.T) {
	src := `
@protocol Readable <NSObject>
- (NSString *)readLine;
@end
`
	fa := extractC(t, "a.h", src, discover.LangObjC)
	for _, s := range fa.Symbols {
		if s.Name == "Readable" && s.Kind != SymInterface {
			t.Errorf("Readable kind = %q, want %q", s.Kind, SymInterface)
		}
	}
}

func TestCDocReadsDoxygenAndSkipsLicenceHeaders(t *testing.T) {
	// Three doc forms are in wide use and all three are read. A plain `/* */` block is
	// as often a licence header as documentation, which is why it is not.
	cases := map[string]string{
		"/** Doxygen form. */\nint a(void);\n":   "Doxygen form.",
		"/*! Alternate form. */\nint a(void);\n": "Alternate form.",
		// A run of /// is one comment, and FirstSentence then cuts it at the first
		// sentence — the same rule every other extractor's doc goes through.
		"/// Line form.\n/// Second line.\nint a(void);\n":             "Line form.",
		"/// Wrapped across\n/// two lines.\nint a(void);\n":           "Wrapped across two lines.",
		"/* Copyright 2026 Somebody. */\nint a(void);\n":               "",
		"/** Summary here.\n * @param x nothing\n */\nint a(int x);\n": "Summary here.",
		"/** Tagged.\n * \\param x nothing\n */\nint a(int x);\n":      "Tagged.",
	}
	for src, want := range cases {
		fa := extractC(t, "a.h", src, discover.LangC)
		if len(fa.Symbols) == 0 {
			t.Fatalf("%q yielded no symbol", src)
		}
		if got := fa.Symbols[0].Doc; got != want {
			t.Errorf("%q -> doc %q, want %q", src, got, want)
		}
	}
}

func TestCDocSurvivesAPreprocessorLineBetween(t *testing.T) {
	// An `#ifdef` guarding a documented function is ordinary, and the doc still belongs
	// to the declaration below it.
	src := `
/** Opens the thing. */
#ifdef HAVE_THING
int thing_open(void);
#endif
`
	fa := extractC(t, "a.h", src, discover.LangC)
	if len(fa.Symbols) == 0 {
		t.Fatal("no symbol")
	}
	if got := fa.Symbols[0].Doc; got != "Opens the thing." {
		t.Errorf("doc = %q", got)
	}
}

func TestCTypeKindsDistinguishClassFromAggregate(t *testing.T) {
	src := `
struct S { int x; };
union U { int a; float b; };
enum E { ONE, TWO };
class C { public: int x; };
`
	fa := extractC(t, "a.hpp", src, discover.LangCpp)
	want := map[string]SymbolKind{
		"S": SymType, "U": SymType, "E": SymType, "C": SymClass,
	}
	for _, s := range fa.Symbols {
		if w, ok := want[s.Name]; ok && s.Kind != w {
			t.Errorf("%s kind = %q, want %q", s.Name, s.Kind, w)
		}
	}
}

func TestCExtractorIsDeterministic(t *testing.T) {
	src := cCorpus()[2].File.Content
	want := extractC(t, "src/session.cc", src, discover.LangCpp)
	for i := 0; i < 3; i++ {
		got := extractC(t, "src/session.cc", src, discover.LangCpp)
		if strings.Join(got.SymbolNames(), ",") != strings.Join(want.SymbolNames(), ",") ||
			strings.Join(got.ImportPaths(), ",") != strings.Join(want.ImportPaths(), ",") {
			t.Fatalf("run %d differs: %v / %v", i, got.SymbolNames(), got.ImportPaths())
		}
	}
}
