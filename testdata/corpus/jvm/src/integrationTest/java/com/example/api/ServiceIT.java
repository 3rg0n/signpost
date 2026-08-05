// Corpus fixture: not compiled, not run.
//
// A second source set declaring a package the main one already declares, which is what makes
// `com.example.api` name two directories. The source set is called `integrationTest` rather
// than `test` on purpose: `test` sorts after `main`, so directory order alone would resolve
// every import of the package to the main copy and the choice would never be exercised.
// Gradle's convention for the extra source set is this name, Android's is `androidTest`, and
// both sort ahead of `main`.
//
// It is also the file that makes the basename rule load-bearing here. No path segment equals
// `test`, so the directory rule does not fire; `ServiceIT` is what marks this a test, and a
// production class misread as one drops out of the public surface it declares.
package com.example.api;

import com.example.store.Repository;

// A real declared dependency signpost cannot see. It reads no pom.xml or build.gradle yet,
// so no JVM manifest states this repository's dependencies and there is no declared list for
// the name to match. Reported as unresolved rather than invented as a Maven coordinate the
// repository never wrote — the gap is visible in the coverage report, which is where a reader
// can see the limitation.
import org.junit.jupiter.api.Test;

public class ServiceIT {
    @Test
    public void returnsEveryRow() {
        assert new Service(new Repository()).all().isEmpty();
    }
}
