# Corpus fixture: not configured, not built.
#
# The top-level build file of the C tree, and the only place this repository says what a C
# project produces: a library, a program, and a test. Nothing else in a C repository states any
# of it — there is no manifest with a `scripts` block and no convention a resolver can lean on.

cmake_minimum_required(VERSION 3.20)
project(corpus_buffer VERSION 1.4.2 LANGUAGES C)

# A found package with a version constraint and REQUIRED: the build does not work without it.
find_package(OpenSSL 3.0 REQUIRED)

# A found package without REQUIRED, which is the optional half of the same command. Both are
# here because the difference is a fact a reader auditing what actually ships acts on, and it
# is stated by nothing but REQUIRED's absence.
find_package(ZLIB)

add_subdirectory(src)
add_subdirectory(tests)

# The include root the C extractor guesses at, declared here where CMake actually states it.
# Read and deliberately recorded nowhere: see the comment on this command in
# internal/manifest/cmake.go, and the gap ADR 0017 names.
include_directories(include)
