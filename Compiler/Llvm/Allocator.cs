using Compiler.Abi;

namespace Compiler.Llvm;

// The allocator every module carries, as plain IR: a block is popped from the list of its size
// class, or cut from the fresh memory after the header, and freeing pushes it back on its list.
// The header is where the host laid it out (Abi/Heap.cs). Nothing here calls anything, so the
// object stays free of anything to link.
public static class Allocator
{
    public const string Heap = "%Heap";


    public static string Emit()
    {
        return $$"""
            {{Heap}} = type { i64, i64, i64, [{{Protocol.SizeClasses}} x ptr] }

            ; Returns a block of the class, or null when the heap is out of memory.
            define internal ptr @bc_alloc(ptr %heap, i64 %class, i64 %bytes) {
            entry:
              %list = getelementptr {{Heap}}, ptr %heap, i32 0, i32 3, i64 %class
              %head = load ptr, ptr %list
              %empty = icmp eq ptr %head, null
              br i1 %empty, label %fresh, label %reuse

            reuse:
              %next = load ptr, ptr %head
              store ptr %next, ptr %list
              br label %done

            fresh:
              %used = load i64, ptr %heap
              %limit.ptr = getelementptr {{Heap}}, ptr %heap, i32 0, i32 1
              %limit = load i64, ptr %limit.ptr
              %end = add i64 %used, %bytes
              %fits = icmp ule i64 %end, %limit
              br i1 %fits, label %cut, label %full

            cut:
              store i64 %end, ptr %heap
              %memory = getelementptr {{Heap}}, ptr %heap, i32 1
              %block = getelementptr i8, ptr %memory, i64 %used
              br label %done

            full:
              ret ptr null

            done:
              %result = phi ptr [ %head, %reuse ], [ %block, %cut ]
              %live.ptr = getelementptr {{Heap}}, ptr %heap, i32 0, i32 2
              %live = load i64, ptr %live.ptr
              %more = add i64 %live, %bytes
              store i64 %more, ptr %live.ptr
              ret ptr %result
            }

            define internal void @bc_free(ptr %heap, ptr %block, i64 %class, i64 %bytes) {
            entry:
              %list = getelementptr {{Heap}}, ptr %heap, i32 0, i32 3, i64 %class
              %head = load ptr, ptr %list
              store ptr %head, ptr %block
              store ptr %block, ptr %list
              %live.ptr = getelementptr {{Heap}}, ptr %heap, i32 0, i32 2
              %live = load i64, ptr %live.ptr
              %less = sub i64 %live, %bytes
              store i64 %less, ptr %live.ptr
              ret void
            }

            """;
    }
}
