# Modules

A program compiles into a module, and a module is two files: an object holding machine code
for one CPU, and a manifest describing how to call it. Together they are the whole of what a
host needs, and this chapter specifies both precisely enough for a host to be written from
it. [hosting.md](hosting.md) then describes the calling convention and the memory a host
provides at run time.

## From source to module

The compiler reads one `.bc` file and prints LLVM IR on its standard output, or the manifest
as JSON when given `--manifest`; it never writes a file. Clang turns the IR into an ELF
relocatable object for a target named on its command line, `aarch64-linux-gnu` or
`x86_64-linux-gnu`. The recipe that ties the two together lives in the justfile shipped next
to the compiler, and produces the object and the manifest next to the source:

```bash
just pod program.bc aarch64      # program.aarch64.o and program.json
just pod program.bc x86_64       # program.x86_64.o
```

The recipe passes clang the options the object format requires, and anyone building modules
another way needs the same ones: `-O2 -c`, `-fno-jump-tables`, `-fno-vectorize
-fno-slp-vectorize`, `-fno-addrsig`, `-fstack-size-section`, and `-ffixed-x18` on `aarch64`.
Each has a reason. Jump tables and vectorised constants would put data in a read-only
section and relocations in the code, both of which the object format forbids.
`-fstack-size-section` records every function's frame size so that a host can size the
stack. `-ffixed-x18` keeps the code off the register that macOS reserves on ARM64, so the
same object runs on Linux and macOS. `-fno-addrsig` drops a section nothing reads. The
justfile in `Compiler/dist/` is the source of truth for the list.

## The object

The object is a relocatable ELF file, `ET_REL`, for the machine `EM_AARCH64` or `EM_X86_64`.
It contains one section that occupies memory, `.text`, which holds every function of the
program and the entries of the external ones; a `.stack_sizes` section, with its
`.rela.stack_sizes`, recording the frame size of every function; the symbol table; and
nothing a loader would have to process. There is no data section, no `.rodata`, no `.bss`,
no relocation into `.text`, no undefined symbol, no dynamic section and no dependency on a C
library. The target triple says Linux, but nothing in the object is specific to it: the code
makes no system call and touches no global, so the same object runs on macOS, and would run
anywhere the CPU is the same and the calling convention is the System V one on x86_64 or the
standard procedure call standard on AArch64.

A host checks these properties before it runs anything, because they are what makes the code
safe to place at any address:

1. The file is `ET_REL` and its machine is the one the host runs on.
2. A `.text` section exists and is not empty.
3. A `.stack_sizes` section exists.
4. No `SHT_RELA` or `SHT_REL` section targets `.text`. Relocations targeting `.stack_sizes`
   are expected and ignored.
5. No section other than `.text` carries the `SHF_ALLOC` flag with a non-zero size.

An object that fails a check is not a module, and the host refuses it. The checks are a
structural guarantee, not a proof: the code could still, in principle, hold any instruction.
What makes it trustworthy is the compiler that emitted it, so a host that runs code it did
not compile itself compiles the source with pinned compiler and clang versions instead of
accepting a binary, as it would refuse a plugin from an unknown build.

The entries are the global symbols of `.text`. Each external function `f` has one, named
`bc_f`, whose value is its offset in the section. Internal functions have no symbol and are
reachable only from inside the code. The manifest says which symbols to expect.

`.stack_sizes` is the section clang emits under `-fstack-size-section`: a sequence of
entries, each an 8-byte address followed by the function's frame size as an unsigned
LEB128. In a relocatable object the addresses are zero with relocations in
`.rela.stack_sizes`. The host ignores them and takes the largest size, which is the one
number it needs.

## The manifest

The manifest is a JSON document printed by the compiler for the same source, and the
contract between the module and its host: it describes every layout the host reads or
writes, and nothing else. The host parses it once when loading and afterwards only passes
memory.

| Key | Content |
|---|---|
| `source` | the name of the source file, quoted in traces |
| `maxDepth` | the number of nested calls the code allows before faulting, fixed by the compiler; the host reads it to size the stack and does not choose it |
| `errors` | the declared errors as `{name, code}`, codes from 1 in declaration order |
| `structs` | every struct by name, as a block |
| `state` | the block of the `state` variables, in declaration order |
| `functions` | every function in declaration order; the position in this list is the number the trace frames refer to |

A block is `{size, fields}`, the size in bytes with trailing padding included, and each
field `{name, type, offset}`. A struct's block names its fields; the block of a function's
results has unnamed fields. A type is spelled `int`, `uint`, `bool`, `error`, the name of a
struct, or `own Name` and `own Name?` for a field that only exists inside the module. A
function entry is `{name}` alone for an internal function. For an external one it adds
`external: true`; `symbol`, the `bc_` name; `args`, the block of the parameters; `results`,
the block of the return values; and `fails: true` when the function is marked `!`, in which
case the last field of `results` has type `error`. An argument field carries `ref: true`
when the parameter is a ref, and the function then writes through that field of the block.

The manifest of a program with one struct, two errors and one external function reads as
follows; the internal `fee_for` is listed by name only, for the traces.

```json
{
  "source": "bank.bc",
  "maxDepth": 100000,
  "errors": [
    { "name": "Frozen", "code": 1 },
    { "name": "Insufficient", "code": 2 }
  ],
  "structs": {
    "Account": {
      "size": 24,
      "fields": [
        { "name": "id", "type": "uint", "offset": 0 },
        { "name": "balance", "type": "uint", "offset": 8 },
        { "name": "frozen", "type": "bool", "offset": 16 }
      ]
    }
  },
  "state": { "size": 0, "fields": [] },
  "functions": [
    { "name": "fee_for" },
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
  ]
}
```

## Layouts

The offsets in the manifest follow the natural alignment rule of C on 64-bit targets, which
[types.md](types.md) states and which LLVM, C, Go and Rust with `repr(C)` all apply: `int`,
`uint` and `error` take 8 bytes at 8-byte alignment; `bool` takes one byte holding 0 or 1;
an owned field is an 8-byte pointer; a struct places its fields in order, each at the next
multiple of its alignment, is aligned to its widest field and padded to that alignment. A
function's `args` block is laid out as a struct of its parameters in order, and `results` as
a struct of its return values followed by the error field when there is one. The manifest is
authoritative: a host that computes its own offsets must get the same numbers, and the
manifest lets it check.

Padding bytes have no meaning to the module, which never reads them. A host that hashes or
compares blocks, as a network of validators does, must decide on a canonical form; the Go
library's `Canonical` check enforces zero padding and 0-or-1 bools for that purpose, and any
host with the same need does the same.
