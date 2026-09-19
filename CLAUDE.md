@~/Skills/general.md
@~/Skills/commit-convention.md
@~/Skills/git-workflow.md

# BlueCode

A programming language for BluePods pods, with its compiler (`Compiler/`, C# on .NET,
`.bc` → LLVM IR) and the host runtime that runs the compiled pods (`Runtime/`, pure Go).
The language is made to be embedded wherever a program runs code on someone else's behalf;
BluePods is its first user and sets the priorities, so every design choice is judged on
determinism, gas metering and the cost of calling a pod from the host.

## Hard constraints

- 64-bit integers only, no floating point, no undefined behaviour in the emitted IR:
  arithmetic is checked, division is guarded, depth and memory are bounded, and every
  failure is either a fault or a language error carrying a trace. Anything that could make
  two nodes compute a different result from the same source is a bug.
- Native only, no wasm. A pod is a freestanding ELF object for one CPU, code only: no data
  section, no relocation, no libc, no linker; the same file runs on Linux and macOS. The
  vectorization flags in the pod recipe are what keep clang from creating a constant pool —
  do not drop them.
- Nothing crosses the host boundary but copies: the host passes plain integer and struct
  blocks by address and reads results the same way. `own` values, refs into pod memory and
  pointers of any kind never appear in an `external` signature; durable state lives inside
  the pod (`state`), owned by an instance.
- No GC, no reference counting, no arena: ownership is decided at compile time (`own`,
  moves, drops), the allocator is emitted into every module, and an instance runs one call
  at a time.

## How the pieces fit

- The compiler prints LLVM IR (or the JSON manifest) on stdout and never writes files. The
  justfile it ships in `dist/` is what turns a `.bc` into a pod with clang; `just publish`
  lays it next to the single-file binary.
- Every function takes hidden gas, depth, trace and instance parameters; only `external`
  functions get a wrapper, a symbol and an ABI entry in the manifest. The manifest is the
  contract between compiler and runtime: layouts follow natural C alignment, and the runtime
  parses nothing else.
- The runtime has zero dependencies and no cgo: it loads the object itself, calls entries
  through an assembly trampoline on a pooled stack sized from the manifest and the object's
  stack-size section, and turns a negative gas into a `Failure` with the trace.

## The test corpus is the specification

`Tests/programs/*.bc` show every feature, one program per theme with a `///` header saying
what it shows; `Tests/programs/errors/*.bc` must each fail to compile, and carry
`// error: <message>` markers matched exactly against the compiler's diagnostics — a
diagnostic without a marker, or a marker without a diagnostic, fails. A language change is
not done until a program shows it, both CPUs compile it without relocation, and a Go test
drives the pod.

Workflow: `just test` at the root is the whole check — it publishes the compiler and runs
everything from there; `just bench` too when the call path moves — the per-call cost is a
feature.

## The reference follows the language

`docs/language/` is the reference: one file per aspect stating what the compiler accepts,
what it refuses and with which message, with `SKILL.md` as the index an agent loads. It is
part of the definition of done: a change to the language, a new rule, a new or reworded
diagnostic, is not finished until the reference says it, in the same PR. `just docs/check`
compiles every `bluecode` block of the reference and checks that every diagnostic it quotes
is one the error programs produce, so a documented refusal always has a program in the
corpus. The README is the pitch, never the reference.

## Working with the owner

The owner writes the code and learns from explanations: explain and propose, write code
only when asked explicitly. Conversation in French, code and comments in English. The
feature pipeline is deliberately not imported here, for the same reason.
