# BlueCode

BlueCode is a small language for code that a program runs on someone else's behalf: a plugin, a smart contract, a pricing rule, a script a user uploads. It reads like Python with types. It compiles to native code through LLVM. And it comes with the things you would otherwise have to bolt onto a language before you could trust it with that job. Two machines running the same code get the same answer. Every function carries a gas budget. Memory and recursion have a ceiling. An error carries a trace back to the line that raised it. The host calls into it the way it calls its own functions, in nanoseconds, with no virtual machine in between and nothing to serialize.

It was written for [BluePods](https://github.com/clemsix6/BluePods), a decentralized cloud whose backends are pods in Rust compiled to WebAssembly, and it is what will replace them. BluePods is the first user and it sets the priorities. Nothing in the language is tied to it, though. Any program that embeds code it did not write has the same wish list: run it fast, run it the same everywhere, cut it off when it has spent its budget, and get a clear answer when it fails.

A compiled BlueCode file is a module: a freestanding ELF object for one CPU that holds code and nothing else, and a manifest that says how to call it. No data section, no relocation, no libc, no imports. The same object runs on Linux and on macOS. The Go runtime in this repository copies it into executable memory and calls its entry points through a short assembly trampoline. Arguments and results sit in plain memory, in the layout a Go struct with the same fields already has, and any language that can call a C function can host a module the same way.

Three directories. `Compiler/`, in C#, turns a `.bc` file into LLVM IR. `Runtime/`, in Go, loads a module and calls it. `Tests/`, also in Go, holds the corpus: the programs in `Tests/programs/` are the language's specification, each feature shown in one of them, and the harness there compiles and runs every one of them for both CPUs.

## Where it fits

The usual options for embedded code each give something up. Lua and its cousins are easy to embed and easy to meter, and pay for that on every instruction. WebAssembly is fast once compiled, but a call means an instance with its own linear memory, a copy in and a copy out, and a 32-bit machine underneath. Gas has to be injected into the bytecode by a separate tool. Overflow wraps silently. Floats are there, waiting to be misused. A trap says nothing about where the code was when it died. A native plugin is fast and free to call, and you trust it completely, because you have no choice.

BlueCode is a native plugin that the compiler makes safe. The guarantees are properties of the language rather than of a runtime wrapped around it. There are no floats and no undefined behaviour to keep out because the language has neither. Integers are 64-bit and every operation on them is checked. Division is guarded. Recursion and memory have a limit the host sets. Gas is a parameter of every function the compiler emits. When something goes wrong it is either an error the code declared, with a trace to the line that raised it, or a fault the code cannot catch, with the same trace. What is left for the host is a loader that verifies the object is code and only code, and then a call.

## Simple on purpose

The syntax is borrowed from the high-level languages people already know, and it stays that way even though what comes out is machine code. Blocks are indentation. The type goes before the name, a function's return type goes before its name, and `and`, `or` and `not` are words, which evaluate both sides rather than short-circuit. There are no pointers, no generics, no classes and no lifetimes to annotate, and a module is one file of structs and functions with no header and no build file next to it. The compiler manages memory. It works out at compile time what owns what and where it gets freed, so the code never frees anything and never leaks. The whole language fits in an afternoon, and the compiler is strict enough that what it accepts does what it says. A program it refuses comes back with diagnostics that each carry a line and a column.

None of that costs anything at run time. The compiler emits LLVM IR and clang optimizes it at `-O2` the same way it would optimize C. What comes out is machine code with no interpreter, no virtual machine and no runtime library under it. The only overhead is the checks the language requires, and each one is a compare and a branch.

## The language

Three scalar types, `int` and `uint` on 64 bits and `bool`, and structs made of them. A function can return several values. That is all a module computes with. There are no floats and no strings, and no arrays yet.

```
struct Account:
    uint id
    uint balance
    bool frozen


error Frozen
error Insufficient
error SameAccount


external def uint! withdraw (ref Account account, uint amount):
    if account.frozen:
        return error.Frozen
    if account.balance < amount:
        return error.Insufficient
    account.balance = account.balance - amount
    return account.balance


external def ()! deposit (ref Account account, uint amount):
    if account.frozen:
        return error.Frozen
    account.balance = account.balance + amount


external def uint! transfer (ref Account from, ref Account to, uint amount):
    if from.id == to.id:
        return error.SameAccount
    uint left = try withdraw(ref from, amount)
    try deposit(ref to, amount)
    return left


external def bool credit (ref Account account, uint amount):
    deposit(ref account, amount) catch err:
        return false
    return true
```

A `ref` parameter is the caller's variable itself, so what `withdraw` writes to the account stays written, and the caller has to say `ref` at the call site too. A ref is taken on a variable or a field. It lives in a parameter or in a local bound once, never in a struct, and it is never returned. That is why it cannot outlive what it names, and why the compiler needs no lifetime analysis. Everything else is passed by value.

Errors are values, in the shape Zig gave them. `error Name` declares one at file level; the compiler numbers them and the manifest gives the host their names back. A function that can fail says so with `!` after its return type and returns either a value or an error. A caller has three ways to handle a failure and no way to ignore one. `try` passes it up to its own caller. `catch value` replaces it on the spot. `catch err:` opens a block that looks at it and must leave the function. Each relay adds a frame, so the host gets a trace from the line that raised the error to the entry it called. A block that catches one error and raises another keeps the first as the cause.

What the code must never do is a fault: divide by zero, compute a value that does not fit in 64 bits, nest calls deeper than the limit, or allocate past the instance's memory. A fault cannot be caught. The call gives up, the results are zero and the host gets the same kind of trace, pointing at the line. Every `+`, `-` and `*` is checked. A cast between `int` and `uint` reinterprets the bits and never truncates.

```
struct Transfer:
    uint from
    uint to
    uint amount
    own Transfer? next


state own Transfer? head
state uint length


external def () enqueue (uint from, uint to, uint amount):
    head = new Transfer(from, to, amount, take head)
    length = length + 1


external def (bool, uint, uint, uint) dequeue ():
    own Transfer first = take head or:
        return false, 0, 0, 0
    head = take first.next
    length = length - 1
    return true, first.from, first.to, first.amount
```

Memory is owned. There is no garbage collector, no reference count and no arena. A value marked `own` lives in memory of its own for as long as the variable or field that owns it, and is freed the moment its owner is reassigned, goes out of scope or gets dropped. It moves rather than copies. Handing it to a function or a field hands it over, and the old name is dead; the compiler checks that, along with moves inside loops and reads after a move. `new T(...)` builds a value in its own memory. `own T?` says the place may hold nothing, `none` empties it, and `take` moves what a field owns out of it and leaves `none` behind, which is the only way to remove something from a structure. An optional is used by binding it. `if ref Node child = ref tree.left:` runs the block when there is a child, and `own Transfer first = take head or:` binds it or runs a block that has to leave. A ref can only point up the stack and nothing else ever points at an owned value, so nothing can outlive it and freeing comes down to the compiler placing the drops.

`state` declares what a module keeps from one call to the next. The state, and the memory `own` values are allocated from, live in an instance the host creates. The queue above is filled by one call and emptied by another. Only functions marked `external` can be called from the host, and their signatures take and return plain values. An `own` value, a ref into the module's memory or a pointer of any kind never crosses.

The files under `Tests/programs/errors/` go through every rule the compiler enforces, one case each:

```
ownership.bc:28:10: error: 'node' was moved
ownership.bc:42:14: error: 'node' is moved inside a loop
ownership.bc:47:10: error: cannot move out of a field: take a ref into it, or take it with 'take'
```

## From a source file to a module

The compiler prints LLVM IR on standard output and writes no file. The justfile it ships in `Compiler/dist/` pipes that output into clang, and `just publish` lays the justfile next to the single-file binary:

```bash
just publish
just -f Compiler/bin/Release/net10.0/osx-arm64/publish/justfile pod Tests/programs/external.bc aarch64    # external.aarch64.o and external.json, next to the source
just -f Compiler/bin/Release/net10.0/osx-arm64/publish/justfile pod Tests/programs/external.bc x86_64
just -f Compiler/bin/Release/net10.0/osx-arm64/publish/justfile ll Tests/programs/external.bc              # the IR, to read
just test
```

The `pod` recipe runs clang at `-O2` in freestanding mode with jump tables and vectorization turned off. Either would create a constant pool, which needs a data section and a relocation, and the loader accepts neither. The recipe also asks clang to record every function's stack frame in a section of the object, which is how the runtime knows how much stack a module needs.

Every function the compiler emits takes hidden parameters for the gas left, the call depth, the trace and the instance, and returns its results together with the gas. An `external` function also gets a wrapper with a fixed C signature, an exported symbol and an entry in the manifest. The manifest is the contract between the compiler and the host. It gives the layout of every struct and of every external function's arguments and results, the size of the state, the error names and the depth limit. Layouts follow natural C alignment, so a struct declared with the same fields in the same order in Go, C or Rust has the same layout without any annotation.

```json
{
  "name": "withdraw",
  "external": true,
  "symbol": "bc_withdraw",
  "fails": true,
  "args": {
    "size": 32,
    "fields": [
      { "name": "account", "type": "Account", "offset": 0, "ref": true },
      { "name": "amount", "type": "uint", "offset": 24 }
    ]
  },
  "results": {
    "size": 16,
    "fields": [
      { "type": "uint", "offset": 0 },
      { "type": "error", "offset": 8 }
    ]
  }
}
```

## The runtime

`Runtime/` is a Go library, and it is the reference host rather than the only possible one. It parses the ELF object itself, maps the code into executable memory and reads the manifest. A call is a jump through an assembly trampoline of a few instructions, on a stack taken from a pool and sized from the manifest's depth limit and the largest frame the object records, with a guard page below it. Arguments and results are Go memory passed by address. A `ref` argument is written in place, so the caller sees the account after the withdrawal in the very struct it passed.

Nothing in the object or the manifest is specific to Go. The entry of every external function is a plain C function with one fixed signature, the blocks it reads and writes are C-layout structs, and the memory it needs, a stack, a trace block and an instance, is described byte for byte in the reference. A host in C, Rust, Zig, C# or Java through its foreign function interface is a few hundred lines, most of them parsing ELF and JSON; [docs/language/hosting.md](docs/language/hosting.md) is written so that it can be done from the document alone, without reading the Go code.

```go
pod, err := bluecode.Load("bank.aarch64.o", "bank.json")
if err != nil {
    return err
}
defer pod.Close()

type Account struct {
    ID, Balance uint64
    Frozen      bool
}

transfer, _ := pod.Function("transfer")
args := struct {
    From, To Account
    Amount   uint64
}{Account{1, 5000, false}, Account{2, 100, false}, 1000}
var results struct {
    Left uint64
    Err  int64
}

gasLeft, err := transfer.Call(unsafe.Pointer(&args), unsafe.Pointer(&results), 1_000_000)
```

A failure comes back as a `*bluecode.Failure` holding the error's name and code, or the fault's, and the trace the module recorded, resolved to function names and line numbers through the manifest. Printed, it looks like this, for a group booking that caught a refused reservation and raised its own error, twice:

```
Abandoned
    errors.bc:87 booked_for_a_group
caused by Rejected
    errors.bc:79 take_seats
caused by Refused
    errors.bc:19 reserve
```

A plain `Call` runs on a throwaway instance. The state starts at zero and whatever the module allocated is gone afterwards. To keep state between calls, the host creates an `Instance` with the memory the module may own, calls through it, reads how many bytes the module holds with `Live`, and closes it when done. An instance runs one call at a time. A module's functions can be called from any number of goroutines on separate instances.

```go
instance, err := pod.NewInstance(bluecode.DefaultHeapSize)
if err != nil {
    return err
}
defer instance.Close()

enqueue, _ := pod.Function("enqueue")
args := struct{ From, To, Amount uint64 }{1, 2, 300}
if _, err := instance.Call(enqueue, unsafe.Pointer(&args), nil, 1_000_000); err != nil {
    return err
}
```

The cost of a call is a feature, and `just bench` measures it after every change to the call path. On an Apple M3 Max a call into a module costs about 11 ns and allocates nothing, whether or not the function is one that can fail. A call that does fail costs about 50 ns and two small allocations on the Go side, for the trace it writes and the Failure value the host receives. That is the whole price of the boundary. A host can call a module inside a loop the way it would call a closure.

## What a host can rely on

- Two machines running the same module on the same input get the same result. No floats, no wrapping arithmetic, no uninitialized memory, and the emitted IR leaves no undefined behaviour for the optimizer to exploit.
- Every failure is accounted for. An error is a value the code declared and the host can name. A fault ends the call with a negative gas that names it. Both carry a trace. A module cannot recurse without end, because the code the compiler emits checks the depth itself; the gas it threads through every call is what will stop a loop once the cost table lands.
- Memory is bounded twice. The stack is sized from the depth limit and the frame sizes the object records; the heap is whatever the host gives the instance. Running out of either is a fault, not a crash.
- Nothing leaks across the boundary. The host reads and writes plain blocks of memory whose layout the manifest describes. The module sees nothing of the host and can call nothing in it.
- The object is code and only code. The loader refuses an object with a data section or with a relocation into its code, which an import would need too. The trust in a module therefore rests on its source and on the compiler. A host that runs code it did not write, a validator for instance, compiles the source itself with pinned compiler and clang versions instead of accepting a binary.

## BluePods

The pods BluePods runs today are Rust compiled to WebAssembly and executed under wazero. Gas is stitched into the bytecode by a separate tool. The input is a FlatBuffers envelope copied in through a host function, and the output goes back the same way. Every execution gets a fresh instance. The pod itself has to carry everything it needs to stand on its own in `no_std`: a global allocator, a panic handler, a dispatcher macro, Borsh for the arguments. That is a lot of ceremony around a hundred lines of business logic. A pod in BlueCode is the functions and nothing else, and its determinism, its gas and its isolation come from the compiler, which is what a network of validators has to agree on anyway.

Measured on the same machine as the benchmark above, the Rust pod costs about 37 ns per call into a warm wazero instance, about 78 µs when the instance is created for the call as the node does, and about 107 ns for each host function the pod calls. The compute itself runs at the same speed on both paths. The difference is all at the boundary, and the boundary is where a pod spends most of its time when a transaction is a few hundred instructions long.

The node still runs WebAssembly pods. Replacing them is what this project was started for, and the runtime's API is what the node will call, but that work has not begun.

## Documentation

The reference lives in [docs/](docs/SUMMARY.md), one chapter per aspect of the language and two for embedding it. It states what the compiler accepts and refuses, with the exact diagnostics, and every code block in it is compiled and every quoted diagnostic checked against the compiler by `just docs/check`, so it cannot drift from the language without the build saying so.

- [Syntax](docs/language/syntax.md), [types](docs/language/types.md), [functions and refs](docs/language/functions.md), [errors and faults](docs/language/errors.md), [ownership and state](docs/language/ownership.md).
- [Modules](docs/language/modules.md), the object and the manifest, and [hosting a module](docs/language/hosting.md), everything a host in any language has to do.
- [.claude/skills/bluecode/SKILL.md](.claude/skills/bluecode/SKILL.md) is the index a coding agent loads, with the rules that trip people up first; it is active as a skill in this repository.

## Where it stands

The compiler, the object format, the manifest and the runtime are complete for the language described above, and every example runs through the tests. What is missing, in the order it will be done:

- Gas is threaded through every call and checked, but nothing charges it yet. The cost table that makes each block of code pay is the next piece. For BluePods it is a consensus rule, so it gets designed with the network rather than here.
- There are no arrays. The only memory of variable size is a structure of owned structs, like the queue and the tree in the examples.
- Every program is compiled and structurally verified for both CPUs, but the x86_64 objects are never executed. The runtime is vetted for amd64 and tested on arm64.
- An instance lives in memory. Persisting a module's state to disk and loading it back is not done.
- The Go library is the only host written so far. The object format and the manifest are all another one needs, and the hosting chapter of the reference is written for that.
