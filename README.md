# BlueCode

BlueCode is a small programming language for the code a program runs on behalf of someone else: a plugin, a smart contract, a rule, a user's script. It reads like Python with types, compiles to native code through LLVM, and comes with what an embedder otherwise has to build around a language: determinism, a gas meter, a bound on memory and on recursion, errors that carry a trace, and a boundary that nothing crosses but copies. The host calls into it as cheaply as into one of its own functions, in nanoseconds, with no virtual machine and no serialization in between.

It was born for [BluePods](https://github.com/clemsix6/BluePods), a decentralized cloud whose backends are pods written in Rust and compiled to WebAssembly, and it is what will replace them. That is the first user and the one that sets the priorities, but nothing in the language is specific to it. The same needs turn up wherever a program embeds code it does not control: run it fast, run it the same everywhere, stop it when it has spent its budget, and get a clear answer back when it fails.

A compiled BlueCode file is called a pod, after the project it comes from. It is a freestanding ELF object for one CPU that contains code and nothing else: no data section, no relocation, no libc, no import of any kind. The same object runs on Linux and macOS. The Go runtime in this repository copies it into executable memory and calls its entry points through a short assembly trampoline, with arguments and results laid out in plain memory that a Go struct with the same fields already matches.

The project is two directories. `Compiler/` is the compiler, written in C#, which turns a `.bc` file into LLVM IR. `Runtime/` is the Go library that loads a pod and calls it. The examples in `Compiler/examples/` are the specification of the language: every feature is shown in one of them, and they are what the runtime's tests run.

## Where it fits

The usual choices for embedded code each give something up. An interpreter such as Lua is easy to embed and easy to meter, and pays for it on every instruction. WebAssembly runs fast once compiled, but a call means an instance with its own linear memory, a copy in and a copy out, and a 32-bit machine underneath; gas has to be instrumented into the bytecode, overflow wraps silently, floats are there to be avoided, and a trap says nothing about where the code was. A native plugin is fast and costs nothing to call, and is trusted code by definition.

BlueCode is a native plugin that the compiler makes safe to run. The properties an embedder needs are properties of the language, not of a runtime wrapped around it: there are no floats and no undefined behaviour to keep out, integers are 64-bit and arithmetic is checked, division is guarded, recursion is bounded, memory is bounded, and gas is a parameter of every function the compiler emits. A failure is either an error the code declared, which carries a trace to the line that raised it, or a fault the code cannot catch, which carries the same trace. What is left for the host is a loader that checks the object is code and only code, and a call.

## Simple on purpose

The syntax is borrowed from the high-level languages people already know, and it stays there even though the output is machine code. Blocks are indentation. Declarations put the type before the name, functions put their return type before their name, and `and`, `or` and `not` are words. There are no pointers, no generics, no classes, no inheritance, no lifetimes to annotate, no header and no build file: a pod is one file of structs and functions. Memory is managed by the compiler, which decides at compile time what owns what and where it is freed, so the code never frees anything and never leaks. The set of concepts is small enough to learn in an afternoon, and the compiler is strict enough that what it accepts does what it says: a refused program comes back with one diagnostic per mistake, each with its position.

That simplicity costs nothing at run time. The compiler emits LLVM IR and clang optimizes it at `-O2` like any C. The result is machine code with no interpreter, no virtual machine and no runtime library under it, only the checks the language demands, which are a compare and a branch each.

## The language

There are three scalar types, `int` and `uint`, both 64-bit, and `bool`, and structs made of them. A function can return several values. That is the whole of what a pod computes with: no floats, no strings, and no arrays yet.

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

A `ref` parameter is the caller's variable itself, so what `withdraw` writes to the account stays written, and the caller has to say `ref` at the call site too. A ref is taken on a variable or a field and lives in a parameter or a local bound once, never in a struct and never returned, which is why it cannot outlive what it names and needs no lifetime analysis. Everything else is passed by value.

Errors are values, in the shape Zig gave them. `error Name` declares one at file level; the compiler numbers them and the manifest gives the host their names back. A function that can fail says so with `!` after its return type and returns either a value or an error. A caller has exactly three ways to deal with a failure and no way to ignore it: `try` relays it to its own caller, `catch value` replaces it on the spot, and `catch err:` opens a block that examines it and must leave the function. Every relay adds a frame, so the host receives a trace from the line that raised the error to the entry it called, and a block that catches one error and raises another keeps the first as the cause.

What the code must not do at all is a fault: dividing by zero, computing a value that does not fit in 64 bits, nesting calls deeper than the limit, or allocating past the instance's memory. A fault is not an error the code can catch. The call gives up, the results are zero, and the host gets the same kind of trace, pointing at the line. Arithmetic is checked on every `+`, `-` and `*`, and a cast between `int` and `uint` is a reinterpretation, never a silent truncation.

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

Memory is owned, without a garbage collector, reference counts or an arena. A value marked `own` lives in memory of its own for as long as the variable or field that owns it, and is freed the moment its owner is reassigned, goes out of scope or is dropped. It moves rather than copies: handing it to a function or a field hands it over, and the old name is dead, which the compiler checks, along with moves inside loops and reads after a move. `new T(...)` builds a value in its own memory, `own T?` says the place may hold nothing, `none` empties it, and `take` moves what a field owns out of it and leaves `none` behind, the only way to remove something from a structure. An optional is used by binding it: `if ref Node child = ref tree.left:` runs the block when there is a child, and `own Transfer first = take head or:` binds it or runs a block that has to leave. Because a ref can only point up the stack and nothing else ever points to an owned value, nothing can outlive it, and freeing is a matter of the compiler placing the drops.

`state` declares what a pod keeps from one call to the next. The state and the memory `own` values are allocated from both live in an instance the host creates, so the queue in the example above is filled by one call and emptied by the next. Only functions marked `external` can be called from the host, and their signatures take and return plain values: an `own` value, a ref into pod memory or a pointer of any kind never crosses.

The files under `Compiler/examples/errors/` list every rule the compiler enforces, one case each:

```
ownership.bc:36:10: error: 'node' was moved
ownership.bc:42:14: error: 'node' is moved inside a loop
ownership.bc:47:10: error: cannot move out of a field: take a ref into it, or take it with 'take'
```

## From a source file to a pod

The compiler prints LLVM IR on standard output and writes no file. The justfile it ships in `Compiler/dist/` pipes that output into clang, and `just publish` lays the justfile and the examples next to the single-file binary:

```bash
cd Compiler && just publish
cd bin/Release/net10.0/osx-arm64/publish
just pod examples/bank.bc aarch64    # bank.aarch64.o and bank.json, next to the source
just pod examples/bank.bc x86_64
just ll examples/bank.bc             # the IR, to read
```

The `pod` recipe runs clang at `-O2` in freestanding mode, with jump tables and vectorization off, because either would create a constant pool that needs a data section and a relocation, and the loader accepts neither. It also asks clang to record the stack frame of every function in a section of the object, which is how the runtime knows the stack a pod needs.

Every function the compiler emits takes hidden parameters for the gas left, the call depth, the trace and the instance, and returns its results together with the gas. An `external` function additionally gets a wrapper with a fixed C signature and an exported symbol, and an entry in the manifest. The manifest is the contract between the compiler and the host: the layout of every struct, of every external function's arguments and results, the size of the state, the error names, the depth limit. Layouts follow natural C alignment, so a struct declared with the same fields in the same order in Go, C or Rust has the same layout without any annotation.

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

`Runtime/` is the reference host: a Go module with no dependency and no cgo. It parses the ELF object itself, maps its code into executable memory, and reads the manifest. Calling a function is a jump through an assembly trampoline of a few instructions, on a stack from a per-pod pool sized from the manifest's depth limit and the largest frame the object records, with a guard page below it. Arguments and results are Go memory passed by address; a `ref` argument is written in place, so the caller sees the account after the withdrawal in the very struct it passed. Nothing in the object format or the manifest is specific to Go, and a host in another language would do the same in a few hundred lines.

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

A failure comes back as a `*bluecode.Failure` holding the error's name and code, or the fault's, and the trace the pod recorded, resolved to function names and line numbers through the manifest. Printed, it reads like this, for a payroll that caught an insufficient balance and raised its own error instead:

```
PayrollUnfunded
    error_handling.bc:74 pay_salary
caused by Insufficient
    error_handling.bc:35 withdraw
    error_handling.bc:55 transfer
```

A plain `Call` runs on a throwaway instance: the state starts out zero and whatever the pod allocated is gone afterwards. To keep state between calls, the host creates an `Instance` with the memory the pod may own, calls through it, reads how many bytes the pod holds with `Live`, and closes it when done. An instance runs one call at a time; a pod's functions can be called from any number of goroutines on separate instances.

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

The cost of a call is a feature and `just bench` measures it after every change to the call path. On an Apple M3 Max, a call into a pod costs about 11 ns, a failing one with its trace about 12 ns, with no allocation. That is the whole price of the boundary: a host can call a pod inside a loop the way it would call a closure.

## What a host can rely on

- Two machines running the same pod on the same input compute the same result. There are no floats, no wrapping arithmetic, no uninitialized memory, no reading past a bound, and the emitted IR has no undefined behaviour left for the optimizer to exploit.
- Every failure is accounted for. An error is a value the code declared and the host can name; a fault ends the call with a negative gas that names it; both carry a trace. A pod cannot loop forever or recurse without end, because gas and depth are checked by the code the compiler emits.
- Memory is bounded twice: the stack by the depth limit and the frame sizes the object records, the heap by the size the host gives the instance. Running out of either is a fault, not a crash.
- Nothing leaks across the boundary. The host reads and writes plain blocks of memory whose layout the manifest describes; the pod sees nothing of the host and can call nothing in it.
- The object holds code and only code. The loader refuses an object with a data section or a relocation into its code, which is also what an import would need, so the trust in a pod rests on its source and on the compiler. A host that runs code it did not write, a validator for instance, compiles the source itself with pinned compiler and clang versions rather than accepting a binary from anyone.

## BluePods

The pods BluePods runs today are Rust compiled to WebAssembly, executed under wazero with gas stitched into the bytecode by a separate tool, a FlatBuffers envelope copied in and out through host functions, and a fresh instance for every execution. The pod itself carries the scaffolding it needs to stand on its own: `no_std`, a global allocator, a panic handler, a dispatcher macro, Borsh for the arguments. A pod in BlueCode is the functions and nothing else, and its determinism, its gas and its isolation come from the compiler instead of from the tooling around it, which is what a network of validators has to agree on anyway.

Measured on the same machine as the benchmark above, the Rust pod costs about 37 ns per call into a warm wazero instance, about 78 µs when the instance is created for the call as the node does, and about 107 ns for each host function the pod calls. The compute itself runs at the same speed on both paths; the difference is entirely at the boundary, which is where a pod spends most of its time when a transaction is a few hundred instructions.

The node still runs WebAssembly pods. Replacing them is what this project was started for, and the runtime's API is what the node will call, but that work has not started.

## Building and testing

You need the .NET 10 SDK, a clang from LLVM (the pod recipe expects the Homebrew one, at its default path), Go 1.27 and [just](https://github.com/casey/just). Each directory has a justfile and `just --list` is its index.

```bash
cd Compiler && just publish     # builds the compiler, lays the pod recipe and the examples next to it
cd ../Runtime
just testdata                   # compiles every example into a pod for both CPUs, into testdata/
just test                       # vets the runtime for both CPUs and runs the tests on those pods
just bench                      # measures the per-call cost
```

A language change is not done until an example shows it, both CPUs compile it into an object without relocation, and a Go test exercises the pod. The error examples under `Compiler/examples/errors/` must each fail with exactly one diagnostic per case. The pods in `Runtime/testdata/` are generated from the examples and are not versioned.

## Where it stands

The compiler, the object format, the manifest and the runtime are complete for the language described above, and every example runs through the tests. What is missing, in the order it will be done:

- Gas is threaded through every call and checked, but nothing charges it yet. The cost table that makes each block of code pay is the next piece. For BluePods it is a consensus rule, so it is designed with the network rather than here.
- There are no arrays. The only memory of variable size is a structure of owned structs, like the queue and the tree in the examples.
- Every example is compiled for both CPUs, but the x86_64 objects have only been built, not run: the runtime is vetted for amd64 and tested on arm64.
- An instance lives in memory. Persisting a pod's state to disk and reloading it is not done.
- The Go runtime is the only host. The object format and the manifest are all another one needs.
