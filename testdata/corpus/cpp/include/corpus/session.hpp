// Corpus fixture: not compiled, not run.
#pragma once

// C++ standard library headers have no extension, so an extensionless angled include is the
// standard library by construction — recognised from its shape rather than from a list that
// would go stale as the standard grows. Neither an edge nor a reported gap.
#include <memory>
#include <string>
#include <vector>

namespace corpus {

// A forward declaration in C++, same rule as C's: it defines nothing.
class Transport;

/// A live connection to a remote peer.
class Session {
public:
    explicit Session(std::string host);
    ~Session();

    bool open();
    void close();
    const std::string &host() const;

private:
    // Private until the next specifier says otherwise. Visibility in C++ is a property of
    // position in the body rather than of the declaration's own line, which is true of
    // nothing else signpost reads — and a class whose members are all read as private
    // reports its entire callable surface as unreachable.
    bool handshake();

    std::string host_;
    std::vector<std::string> log_;
};

// A struct's members are public with no keyword, which is the one way a struct and a class
// differ. Read backwards, a struct's whole interface disappears.
struct Endpoint {
    std::string host;
    int port;

    std::string describe() const;
};

// C++ style puts the opening brace below a wrapped head, and a base-class list is written
// one base per line. The lookahead for that brace has to be wide enough for a real list:
// bounded at five lines, this class yielded no symbol at all and appeared nowhere in the
// bundle. What stops a forward declaration from reaching a later type's brace is the
// semicolon that ends it, not the width of the window.
class MultiplexedSession final
    : public Session,
      public Transport,
      public Loggable,
      public Observable,
      public Cancellable,
      public Drainable
{
public:
    void drain();
};

}  // namespace corpus
