# Corpus fixture: not configured, not built.
#
# The negative boundary that matters most in this tree, and it works only because it is in a
# third file: `corpus_buffer_core` is declared in `../src/CMakeLists.txt`, which this reader
# never sees, and `cmocka` is declared nowhere in this repository at all. Both are linked here
# by bare name in the same command, so a rule that got either half wrong shows up as one of two
# defects — a library this repository builds reported as a third-party dependency, or a genuine
# third-party dependency dropped from the supply chain.

add_executable(buffer_test buffer_test.c)
target_link_libraries(buffer_test PRIVATE corpus_buffer_core cmocka)

enable_testing()

# The only place any file in this repository says a C binary is a test. `isTestPath` reaches the
# same conclusion from the directory name, which is a guess; this is the project stating it.
add_test(NAME buffer_roundtrip COMMAND buffer_test --verbose)
