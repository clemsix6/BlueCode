# Hosting a module

A host is the program that loads a module and calls it. It can be written in any language
that can map memory as executable, call a machine-code address with the platform's C calling
convention, and lay out a struct as C would. This chapter is the specification a host
follows: the entry signature, the memory the host provides, the protocol by which a call
reports failure, and the steps of a call. The Go library that closes the chapter implements
all of it and is the reference.

## The entry

Every external function is entered through its `bc_` symbol with one signature, the same for
every function of every module:

```
int64 bc_name(void *args, void *results, void *trace, void *instance, int64 gas)
```

The convention is the platform's C convention: System V on x86_64, with the arguments in
`rdi`, `rsi`, `rdx`, `rcx` and `r8` and the result in `rax`; the standard procedure call
standard on AArch64, with the arguments in `x0` to `x4` and the result in `x0`. `args` points
at a block laid out as the manifest's `args` for the function, which the host has filled.
`results` points at a block laid out as `results`, which the entry fills. `trace` and
`instance` point at memory described below. `gas` is the budget. The return value is the gas
left. The entry reads the arguments from the block, calls the function, writes its values into
the results block, and returns.

A `ref` parameter is not copied out of the block. The function receives the address of its
field in the block and works there, so after the call the block holds what the function
left, and that is how a host reads a value written through a ref. The block must therefore
be writable and stay valid for the whole call, as must `results`, `trace` and `instance`.

The code never allocates on any heap but the instance's, never calls out, never blocks and
never touches memory outside the five regions it was given, its own stack included. On
`aarch64` it never uses `x18`.

## The stack

The code runs on whatever stack the stack pointer names when the entry is called, and it
needs a known amount of it. The program's calls nest at most `maxDepth` deep, and the largest
frame of any function is the largest size in `.stack_sizes`. A host that lets the code run
on the calling thread's stack must be certain that stack has room for `(maxDepth + 2) ×
(largest frame + 16)` bytes on top of what the host itself uses. Since `maxDepth` is a
constant of the language and not a choice of the host, that number exceeds a default thread
stack by a wide margin, and the Go library does not take the risk: it gives every call a
stack of its own, mapped at exactly that size rounded up to pages, with a guard page below
it, and switches to it in a few instructions of assembly before jumping to the entry. A host
in C can do the same with `makecontext` and `swapcontext`, with a small assembly thunk, or by
running calls on a thread created with that stack size.

The stack pointer is 16-byte aligned at the call on both CPUs. Nothing on the stack survives
the call.

## The trace

The trace is a block the host provides and the code writes on failure paths only. Its layout
is a count followed by frames of three 64-bit words:

| Offset | Size | Content |
|---|---|---|
| 0 | 8 | count: the number of frames the failure went through |
| 8 + 24 × i | 8 | frame i: the function's number, its index in the manifest's `functions` |
| 16 + 24 × i | 8 | the line in the source |
| 24 + 24 × i | 8 | the code of the error raised on this frame, negative for a fault, zero on a frame that only passed the failure on |

The capacity is `maxDepth + 1` frames, so the block is `8 + 24 × (maxDepth + 1)` bytes.
When more frames would be written than fit, the count still grows and the extra frames are
dropped, so a host reads `min(count, capacity)` frames and knows how many were lost. The host
need not zero the trace between calls, since a failure starts by resetting the count. The
frames run from the one that raised the failure to the entry the host called, and a catch
block that raised a new error appended its frames after the ones it caught, which
[errors.md](errors.md) explains and the Go library prints as `caused by`.

## The instance

The instance is the memory the module owns: its `state` variables and the heap its `own`
values are allocated from. The host lays it out and hands the same block to every call that
should see the same state. A fresh, zeroed block is a module with no history.

| Region | Where | Size |
|---|---|---|
| state | offset 0 | `state.size` from the manifest |
| heap header | the state's size rounded up to a multiple of 8 | three 64-bit words, then one 64-bit pointer per size class |
| heap memory | right after the header | whatever the host decides |

The three words of the header are `next`, the number of bytes of heap memory already cut,
zero at first; `limit`, the size of the heap memory, which the host writes and the module
never changes; and `live`, the bytes currently held by the module's values, which the host
may read to know how much a module holds. The pointers that follow are the heads of the free
lists, null at first. The allocator is part of the module's code. It serves a block of a size
class, powers of two from a minimum, from the free list of that class, or cuts it from the
fresh memory after the header when the list is empty, and faults with `out of memory` when
`next + size` would exceed `limit`. A freed block goes back to the head of its class's list,
and each free block's first word links to the next. The number of classes and the minimum
block size are constants of the compiler, fixed in `Compiler/Abi/Protocol.cs` and
`Compiler/Abi/Heap.cs`: 17 classes from 16 bytes, so the header is 160 bytes and the largest
block a value can take is one megabyte.

To reset an instance, the host zeroes the state and the header and writes `limit` again. The
heap memory itself need not be zeroed, since `new` writes every field.

## Gas and depth

`gas` is a 64-bit integer the host passes in and receives back. Inside the module every
function passes the gas to its callees, gets it back and checks it after each call: a
negative value means the callee gave up, and the function gives up in turn with the same
value. A fault is reported the same way, by a negative gas that names it.

| Gas returned | Fault |
|---|---|
| -1 | too deep |
| -2 | out of gas, which no module produces yet |
| -3 | division by zero |
| -4 | overflow |
| -5 | out of memory |

The codes are fixed in `Compiler/Abi/Protocol.cs`, which the Go library mirrors. The host's
rule is simple: a non-negative return is a completed call, a negative return is an aborted one
whose value names the fault, with zeros in the results block. Nothing charges gas yet, so the
budget comes back unchanged from a completed call. The cost table that will charge each block
of code is the next piece of the language, and a host that meters today counts calls, not
instructions.

Depth is not the host's concern. The entry passes `maxDepth` to the function, which
decrements it at every call and faults with `too deep` when it reaches zero. The limit is a
constant of the compiler written into every module; the host reads it from the manifest to
size the stack and the trace, and cannot choose it.

## Reading the result of a call

After the entry returns, the host looks at three things in this order. First the returned
gas: negative means a fault, and the trace says where. Then, for a function marked `!`, the
last field of the results block: a non-zero value is the code of the error the function
returned, to look up in the manifest's `errors`, and the trace holds its frames; zero is
success. Then the results block itself, whose fields are the return values, and the `args`
block for anything written through a ref. On a fault or an error the values are not
meaningful: the entry wrote zeros on a fault, and on an error the function returned no values.
What was written through a ref before the failure stays written, which a host that wants
transactional semantics handles by working on a copy.

## Writing a host

These are the steps, in the order the Go library performs them.

1. Read the object. Check the five properties of [modules.md](modules.md), read the largest
   frame from `.stack_sizes`, collect the global symbols of `.text` with their offsets.
2. Read the manifest. For every external function, find its symbol among the object's.
   Compute the stack size, `(maxDepth + 2) × (largest frame + 16)` rounded up to pages, and
   the trace size, `8 + 24 × (maxDepth + 1)`.
3. Copy `.text` into a fresh anonymous mapping, make it read-only and executable, and on ARM
   flush the instruction cache over that range, since its data and instruction caches are
   not coherent. The code is position independent and any address works. Where the system
   forbids memory that is writable and executable at once, map it writable, write, then
   change the protection.
4. For each instance the host wants, allocate the state rounded up to 8 bytes, plus the
   header, plus the heap, zeroed, and write `limit`.
5. For each call, fill an `args` block as the manifest says; provide a `results` block, a
   trace block and a stack; call the entry with the C convention on that stack; read the
   outcome as the previous section says.

A host that runs calls concurrently gives each call its own stack and trace and never lets
two calls share an instance at once. The code itself is reentrant. Because all of this is
reading two files and following C conventions, a host in C, Rust, Zig, C# or Java through
its foreign function interface is a matter of a few hundred lines, most of which parse ELF
and JSON. The delicate part is the stack, and the simplest correct answer in a language
without stack switching is one thread per concurrent call, created with the stack size
computed in step 2.

## The Go library

`Runtime/` is the reference host, a Go module with no dependency and no cgo. It performs the
steps above with `debug/elf`, `encoding/json` and the `syscall` mapping calls, plus a
trampoline of a dozen assembly instructions per CPU that saves the Go stack pointer in a
callee-saved register, switches to the call's stack, calls the entry and switches back. Its
stacks come from a pool, one per call in flight, each mapping holding a throwaway instance,
the trace, the guard page and the stack, so that a call made without an instance of the
host's costs no allocation. The library keeps the name `Pod` for a loaded module, after the
project the runtime was first written for.

| Call | Meaning |
|---|---|
| `bluecode.Load(objectPath, manifestPath)`, `LoadBytes(object, manifest)` | reads both, refuses an object for another CPU, with relocations in its code, with a data section or without `.stack_sizes`, and maps the code executable |
| `pod.Function(name)` | an external function; an internal one is refused by name |
| `fn.Call(args, results, gas)` | runs on a throwaway instance wiped before the call; `args` and `results` point at Go memory laid out as `fn.Args` and `fn.Results` say, `results` may be nil when that block is empty; returns the gas left and a `*Failure` on an error or a fault |
| `pod.NewInstance(heapSize)`, `instance.Call(fn, args, results, gas)`, `instance.Live()`, `instance.Close()` | an instance that keeps state and heap between calls, the bytes it holds, and its release |
| `Failure{Name, Code, Source, Trace, Lost}`, `Frame{Function, Line, Raised}` | the error's name and code, negative for a fault, the source file, the trace resolved through the manifest, and the frames lost to capacity; `Error()` prints it with each earlier error as a `caused by` |
| `pod.Manifest()`, `pod.StackSize()` | the parsed manifest, and the stack every call runs on |
| `manifest.Canonical(block, data)` | checks that `data` is the one valid encoding of a block: exact size, bools 0 or 1, padding zero, no owned field |
| `pod.Close()` | releases the code |

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

withdraw, _ := pod.Function("withdraw")
args := struct {
    Account Account
    Amount  uint64
}{Account{1, 5000, false}, 1000}
var results struct {
    Left uint64
    Err  int64
}

gasLeft, err := withdraw.Call(unsafe.Pointer(&args), unsafe.Pointer(&results), 1_000_000)
```

A module's functions may be called from any number of goroutines, each call taking a stack
from the pool. An instance is used by one call at a time, and the library does not
serialise them: keeping two calls off one instance is the caller's job.
