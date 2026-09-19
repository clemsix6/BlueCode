# The host boundary

## Pods

A pod is one `.bc` file compiled by the compiler into LLVM IR and by clang into a
freestanding ELF object for one CPU, `aarch64` or `x86_64`, plus a JSON manifest. The object
holds code and nothing else: no data section, no relocation into the code, no import. The
same object runs on Linux and macOS. `just pod file.bc [cpu]` from the compiler's publish
directory produces `file.<cpu>.o` and `file.json` next to the source; `Compiler/dist/justfile`
is the source of truth for the clang flags.

## What crosses

Only `external` functions are callable. Their arguments form one block laid out like a struct
of the parameters; their results form one block of the return types, followed by an `error`
field when the function is marked `!`. The host writes the arguments block, calls, and reads
the results block. A `ref` parameter is worked on in place: after the call, the arguments
block holds what the function left there, on failure too. Nothing else crosses: no `own`, no
ref into pod memory, no pointer, and a pod has no way to call the host.

Layouts follow natural C alignment: `int`, `uint` and `error` are 8 bytes aligned on 8, `bool`
is 1 byte, a struct is aligned on its widest field and padded to it. A Go, C or Rust struct
with the same fields in the same order has the same layout.

## The manifest

| Key | Content |
|---|---|
| `source` | the file name, for the lines of a trace |
| `maxDepth` | the call depth the code enforces |
| `errors` | `{name, code}` per declared error, codes from 1 |
| `structs` | per struct, its `size` and `fields` as `{name, type, offset}` |
| `state` | the block of the `state` variables, in declaration order |
| `functions` | every function in declaration order, which numbers them for the traces; an external one also has `external`, `symbol`, `args`, `results`, and `fails` when marked `!` |

A type is spelled `int`, `uint`, `bool`, `error`, the name of a struct, or `own Name` and
`own Name?` for fields that exist inside the pod only. An argument carries `ref: true` when
the parameter is a ref.

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

## The entry

Every external function has a symbol `bc_<name>` with one signature for all pods:

```
int64 bc_name(void *args, void *results, void *trace, void *instance, int64 gas)
```

It returns the gas left, negative when the function gave up, in which case the results are
all zero. A function that can fail writes its error code, zero on success, in the last field
of the results. The fault a negative gas names is fixed by `Compiler/Abi/Protocol.cs`, which
the runtime mirrors: too deep, out of gas, division by zero, overflow, out of memory.

Inside the pod every function, external or not, takes the gas, the depth allowed, the trace
and the instance after its parameters, and returns its values, its error if it can fail, and
the gas, as one aggregate. The wrapper behind the symbol hides that.

## Gas and depth

Every call passes the gas left down and gets it back; a function that finds its gas negative
after a call gives up in turn, so a fault unwinds to the entry with its trace. The depth
allowed is decremented on every call and the call faults with `too deep` past the manifest's
limit. The host sizes the stack from that limit and the largest frame the object records in
its `.stack_sizes` section, so a stack overflow cannot happen.

Nothing charges gas yet: the budget passed in comes back unchanged unless a fault names one.
The cost table is the next piece of the language.

## Traces

The trace is a block the host provides: a count, then frames of three 64-bit words, the
function's number, the line, and the code of the error raised on that frame, zero on a frame
that only passed the failure on. It is written on failure paths only, from the frame that
raised the failure to the entry the host called. Its capacity is the depth limit plus one
frame; the count keeps growing when the frames are full, so the host knows how many were
lost.

## Instances

An instance is the memory a pod owns between calls: its `state` block, then the heap header,
then the heap. The host allocates it zeroed and passes it to every call. The allocator is
part of the pod: blocks come in power-of-two size classes from a minimum, with one list of
freed blocks per class, and `new` faults with `out of memory` when its class has no freed
block and the fresh memory is spent. `Compiler/Abi/Heap.cs` and `Protocol.cs` fix the header
and the classes. The header records the bytes currently owned, which the host reads. An
instance runs one call at a time.

## The Go runtime

`Runtime/` is the reference host: a Go module with no dependency and no cgo.

| Call | Meaning |
|---|---|
| `bluecode.Load(objectPath, manifestPath)`, `LoadBytes(object, manifest)` | parses both, refuses an object for another CPU, with relocations in its code, with a data section or without `.stack_sizes`, and maps the code executable |
| `pod.Function(name)` | an external function; an internal one is refused by name |
| `fn.Call(args, results, gas)` | runs on a throwaway instance wiped before the call; `args` and `results` point at Go memory laid out as `fn.Args` and `fn.Results` say, `results` may be nil when the block is empty; returns the gas left and a `*Failure` on an error or a fault |
| `pod.NewInstance(heapSize)`, `instance.Call(fn, args, results, gas)`, `instance.Live()`, `instance.Close()` | an instance that keeps state and heap between calls, the bytes it owns, and its release |
| `Failure{Name, Code, Source, Trace, Lost}`, `Frame{Function, Line, Raised}` | the error's name and code (negative for a fault), the file, the trace as the pod recorded it, resolved through the manifest, and the frames lost to capacity; `Error()` prints it with each earlier error as a `caused by` |
| `pod.Manifest()`, `pod.StackSize()` | the parsed manifest, the stack every call runs on |
| `manifest.Canonical(block, data)` | checks that `data` is the one valid encoding of a block: exact size, bools 0 or 1, padding zero, no owned field; for a host that hashes what it passes |
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

A pod's functions may be called from any number of goroutines; each call takes a stack from
the pod's pool. An instance is used by one call at a time, which the host enforces.
