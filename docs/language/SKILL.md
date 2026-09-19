---
name: bluecode
description: Reference for BlueCode, the language of BluePods pods (.bc files). Syntax, types, functions and refs, errors and faults, ownership and state, the host boundary. Use when writing or reviewing a .bc program, when one fails to compile, or when a host has to call a pod.
---

# BlueCode reference

BlueCode is a small statically typed language that compiles to a native pod a host calls
from Go. It has 64-bit integers, bools and structs, functions with several results, refs,
errors as values, compile-time ownership and per-instance state. It has no floats, no
strings, no arrays, no pointers, no globals other than `state`, and no way to call the host.

Each file states what the compiler accepts, what it refuses and with which message. The
programs under `Tests/programs/` are the executable specification; `Tests/programs/errors/`
holds the refused cases, one per rule, with the message as a `// error:` marker on the line.

| File | Covers |
|---|---|
| [syntax.md](syntax.md) | file layout, indentation, comments, literals, operators, statements |
| [types.md](types.md) | int, uint, bool, structs, construction, casts, several results |
| [functions.md](functions.md) | def, external, parameters, ref, calls, return, scope |
| [errors.md](errors.md) | error, !, try, catch, traces, faults |
| [ownership.md](ownership.md) | own, new, none, take, moves, drops, bindings, the frozen rule, state |
| [host.md](host.md) | pods, manifest, entry ABI, gas, traces, instances, the Go runtime |

## The rules that bite

1. Indentation is spaces only. A block opens with `:` at the end of the line and holds at least one statement.
2. The type comes first, everywhere: `int x = 1`, `def uint f (uint n):`, `ref Account a`.
3. A literal takes the type of its context and is `int` otherwise. `int` and `uint` never mix; cast with `int(x)` or `uint(x)`, which reinterprets the bits.
4. `and` and `or` evaluate both sides. Guard a division or a subtraction with an `if`, never with `and`.
5. A function with results ends every path on a `return`; a `while` never counts as such a path.
6. A call is a statement only when it returns nothing. A value that is not used is an error.
7. A ref parameter takes `ref place` at the call site; a value parameter never does.
8. A call that can fail is never bare: `try f()`, `f() catch value`, or `f() catch err:` with a block that leaves the function.
9. Faults, division by zero, overflow, depth, memory, cannot be caught. Check before computing.
10. An `own` value moves: after `g(node)` or `x = node`, `node` is dead. A field or a `state` is never moved out of: `ref` into it, or `take` it.
11. An `own T?` is used by binding it: `if ref T x = ref place:`, or `T x = ... or:` with a block that leaves.
12. While a ref variable points into a place, that place is frozen: no assignment, move, take or pass by ref.
13. Only `external` functions are visible to the host, and nothing that owns memory appears in their signature.
14. Every `+`, `-` and `*` is overflow-checked. `-x` is `int` only.

## Checking a program

The published compiler prints IR on stdout and diagnostics on stderr, one per line as
`file:line:column: error: message`, and exits with 1 when there is any:

```
Compiler/bin/Release/net10.0/osx-arm64/publish/Compiler program.bc > /dev/null
```

`just docs/check` from the repository root compiles every `bluecode` block of this reference
and checks every diagnostic it quotes against what the error programs make the compiler say.
