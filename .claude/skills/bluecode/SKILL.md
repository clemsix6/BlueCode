---
name: bluecode
description: Reference for the BlueCode language (.bc files) and for hosting the modules it compiles to. Syntax, types, functions and refs, errors and faults, ownership and state, the object and manifest formats, the calling convention. Use when writing or reviewing a .bc program, when one fails to compile, or when a host in any language has to load and call a module.
---

# BlueCode reference

BlueCode is a small statically typed language for code that a program runs on someone
else's behalf. A program is one `.bc` file; it compiles to a module, an object of machine
code for one CPU plus a manifest, that a host loads and calls through a fixed C signature.
The language has 64-bit integers, bools and structs, functions with several results, refs,
errors as values, ownership decided at compile time and per-instance state. It has no
floating point, no strings, no arrays, no pointers, no globals other than `state`, and no
way for the code to call the host. Determinism, checked arithmetic, bounded depth and memory
and a trace on every failure are properties of the language, not of a runtime around it.

Each chapter below is written to be read on its own; the paths are from the root of the
BlueCode repository, and the same files are indexed for a human reader in `docs/SUMMARY.md`. The first five state the language, what
the compiler accepts, what it refuses and with which message, and end with a table of those
messages. The last two are the embedding specification: a host in any language can be
written from them.

| Chapter | Covers |
|---|---|
| [syntax.md](docs/language/syntax.md) | file layout, indentation, comments, literals, operators, statements, scope |
| [types.md](docs/language/types.md) | int, uint, bool, structs, construction, casts, layout, several results |
| [functions.md](docs/language/functions.md) | def, external, parameters, ref, ref variables, calls, return |
| [errors.md](docs/language/errors.md) | error, !, try, catch, traces, faults |
| [ownership.md](docs/language/ownership.md) | own, new, none, take, moves, drops, bindings, the frozen rule, state |
| [modules.md](docs/language/modules.md) | the pipeline, the object and its checks, the manifest, layouts |
| [hosting.md](docs/language/hosting.md) | the entry signature, stack, trace, instance, gas, failure protocol, writing a host, the Go library |

The programs under `Tests/programs/` are the executable specification and show every
feature in use; `Tests/programs/errors/` holds the refused cases with the message as a
`// error:` marker on the line, and every message this reference quotes is one of them.

## The rules that bite

1. Indentation is spaces only. A block opens with `:` at the end of the line and holds at least one statement.
2. The type comes first, everywhere: `int x = 1`, `def uint f (uint n):`, `ref Account a`.
3. A literal takes the type of its context and is `int` otherwise. `int` and `uint` never mix; cast with `int(x)` or `uint(x)`, which reinterprets the bits.
4. `and` and `or` evaluate both sides. Guard a division or a subtraction with an `if`, never with `and`.
5. A function with results ends every path on a `return`; a `while` never counts as such a path.
6. A call is a statement only when it returns nothing. A value that is not used is an error.
7. A ref parameter takes `ref place` at the call site; a value parameter never does.
8. A call that can fail is never bare: `try f()`, `f() catch value`, or `f() catch err:` with a block that leaves the function. `catch` only follows a call that is the whole value of a declaration, an assignment or a statement, never `return`; `try` goes anywhere.
9. Faults, division by zero, overflow, depth, memory, cannot be caught. Check before computing.
10. An `own` value moves: after `g(node)` or `x = node`, `node` is dead. A field or a `state` is never moved out of: `ref` into it, or, when it is an `own T?`, `take` it.
11. An `own T?` is used by binding it: `if ref T x = ref place:`, or `T x = ... or:` with a block that leaves.
12. While a ref variable points into a place, that place is frozen: no assignment, move, take or pass by ref.
13. Only `external` functions are visible to the host, and nothing that owns memory appears in their signature.
14. Every `+`, `-`, `*`, `/` and `%` is checked. `-x` is `int` only.

## Checking a program

The published compiler prints IR on stdout and diagnostics on stderr, one per line as
`file:line:column: error: message`, and exits with 1 when there is any:

```
Compiler/bin/Release/net10.0/osx-arm64/publish/Compiler program.bc > /dev/null
```

`just docs/check` from the repository root compiles every `bluecode` block of this reference
and checks every diagnostic it quotes against what the error programs make the compiler say.
