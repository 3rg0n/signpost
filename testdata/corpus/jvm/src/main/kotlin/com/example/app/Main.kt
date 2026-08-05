// Corpus fixture: not compiled, not run.
package com.example.app

// Cross-language, and the reason the JVM subtree holds both: a Kotlin file importing a Java
// package is ordinary in every JVM repository, and the two extractors share one resolution
// map because the compiler does.
import com.example.api.Service

// A subpackage of one already declared, and `com.example.store` is deliberately *not*
// imported here. Matched shortest-first this lands on the parent, so the edge points at the
// package containing the one that was asked for — and with both imported, the wrong answer
// and the right one draw the same pair of edges and nothing can tell them apart. The store
// is reached through the service instead, which is what the absence of that import means.
import com.example.store.internal.Cache

// `kotlin.*` is the Kotlin standard library — shipped and patched with the toolchain, so no
// node and no gap.
import kotlin.math.max

// `kotlinx.*` is the neighbour that is not, and the pair is here so the difference is
// measured rather than asserted. Coroutines, serialization and datetime are separately
// versioned artifacts with their own release cadence and their own advisories, so a first
// segment matched as a prefix rather than as a whole segment reports this line as the
// standard library — the one classification that makes a dependency disappear from the
// coverage report instead of appearing in it as a gap.
import kotlinx.coroutines.runBlocking

/** Wires a service to its cache. */
class App(private val service: Service) {
    val cache = Cache()

    /** Returns the longest greeting's length, or zero. */
    fun widest(): Int = service.all().fold(0) { acc, s -> max(acc, s.length) }

    /** Drops everything cached. */
    fun reset() = cache.clear()
}

fun main() = runBlocking {
    println(App(Service.forStore()).widest())
}
