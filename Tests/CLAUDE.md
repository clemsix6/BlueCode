@~/Skills/go-style.md

`Tests/programs/` is the language's specification: one `.bc` program per theme, each with a
`///` header saying what it shows, and `Tests/programs/errors/` holds the programs that must
never compile, each carrying `// error: <message>` markers the harness matches exactly
against the compiler's diagnostics. Every program is built into a pod through the recipe the
compiler's justfile ships, never through clang directly, so the pod a test runs is the same
one a host would get. The Go test cache stays disabled for this module: it cannot see the
compiler, clang or the programs changing.
