# Corpus fixture: not configured, not built.
#
# The library this repository builds, declared in the subdirectory's own build file — which is
# the ordinary layout of a C project and the reason `Facts.Targets` exists. The file above only
# calls `add_subdirectory(src)`; it never sees this line, and `tests/CMakeLists.txt` links
# `corpus_buffer_core` by bare name with nothing in the syntax saying where it comes from.
add_library(corpus_buffer_core STATIC buffer.c)

# A namespaced link target belonging to a package the top-level file found. It must reduce to
# `OpenSSL` and appear once, not twice under two scopes.
target_link_libraries(corpus_buffer_core PUBLIC OpenSSL::SSL)
