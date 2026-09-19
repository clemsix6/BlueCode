set quiet

# Builds the compiler the tests compile the programs with.
publish:
    just Compiler/publish

# Publishes the compiler, vets and tests the runtime, checks the reference, then builds and runs every program.
test: publish
    just Runtime/test
    just docs/check
    cd Tests && go test -count=1 -v ./...

# Publishes the compiler and measures the per-call cost, which is a feature of the runtime.
bench: publish
    cd Tests && go test -run '^$' -bench . -benchmem
