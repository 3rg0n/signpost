// Corpus fixture: not compiled, not run.

// The project's own public header, through the include root nearest this file.
#include <corpus/session.hpp>

// The C++ standard library, by shape.
#include <sstream>
#include <utility>

// A C standard header used from C++ through its `c`-prefixed wrapper. It has no extension,
// so it is stdlib by the same shape rule as the rest — which is why the extensionless rule
// covers the whole standard library and not just the C++-native part of it.
#include <cstring>

// The near-miss for C++, and the one a shape-based stdlib rule is most exposed to. A test
// framework's header is angled because it is on the include path, and it is a dependency
// with its own advisories rather than the platform. It has an extension, which is the only
// thing separating it from `<memory>` above — and it must reach the gap report.
#include <gtest_extras/matchers.h>

namespace corpus {
namespace {

// An anonymous namespace is `static` spelled as a scope: everything in it has internal
// linkage and is not link surface.
int connection_counter = 0;

std::string trim(const std::string &s)
{
    return s;
}

}  // namespace

// An out-of-line member definition names its own owner, which is the only place a receiver
// can come from when the class body is in another file. Its visibility is not here — the
// access specifier governing it is a line in the header — so this definition claims nothing
// about it and the declaration in the class body stays the authority.
Session::Session(std::string host) : host_(std::move(host)) {}

Session::~Session() = default;

bool Session::open()
{
    if (handshake()) {
        ++connection_counter;
        return true;
    }
    return false;
}

void Session::close()
{
    connection_counter = 0;
}

const std::string &Session::host() const
{
    return host_;
}

bool Session::handshake()
{
    return !trim(host_).empty();
}

std::string Endpoint::describe() const
{
    std::ostringstream out;
    out << host << ":" << port;
    return out.str();
}

// C++11 spells an attribute with no keyword at all, and it lands in front of the return type
// where it moves the first parenthesis on the line — the one every declaration rule reads as
// the parameter list.
[[nodiscard]] int connection_count()
{
    return connection_counter;
}

// A namespace alias opens no scope. Pushing one would leave a scope open for the rest of the
// file and claim every later declaration as a member of it.
namespace alias = corpus;

}  // namespace corpus
