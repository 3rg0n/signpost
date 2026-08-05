// Corpus fixture: not compiled, not run.
package com.example.store;

import java.util.ArrayList;
import java.util.List;

// The `javax` split, which is the one JVM prefix with no rule: the namespace was divided
// between the platform and Java EE in 1999 and the division is historical, not structural.
// `javax.crypto` is in the JDK and `javax.servlet` is a Maven artifact somebody chose and
// must upgrade, so the two sit here together — matched on the first segment they are both the
// runtime, which hides a real supply-chain fact behind the words "standard library", and
// matched neither way the JDK's own packages are reported as gaps on every JVM repository.
import javax.crypto.Cipher;
import javax.servlet.http.HttpServletRequest;

/** Stored greetings. */
public class Repository {
    private final List<String> rows = new ArrayList<>();

    /** Returns every row. */
    public List<String> find() {
        return rows;
    }
}
