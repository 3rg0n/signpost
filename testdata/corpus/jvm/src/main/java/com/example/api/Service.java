// Corpus fixture: not compiled, not run.
package com.example.api;

import com.example.store.Repository;

// The near-miss for JVM package matching. `com.example.apiv2` shares every character of
// `com.example.api` and is not under it — a package is dot-delimited, so a prefix compared
// as a string folds a sibling package into this one and draws an edge to code that never
// declared the name. Nothing in this tree declares it.
import com.example.apiv2.Legacy;

// The runtime. `java.*` is the JDK, shipped and patched with the toolchain, so no node and
// no reported gap.
import java.util.List;

/** The corpus's Java service. */
public class Service {
    private final Repository repo;

    public Service(Repository repo) {
        this.repo = repo;
    }

    /** Builds a service over a fresh store. */
    public static Service forStore() {
        return new Service(new Repository());
    }

    /** Returns every stored greeting. */
    public List<String> all() {
        return repo.find();
    }

    void packagePrivate() {}
}
