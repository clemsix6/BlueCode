package bluecode_test

import (
	"fmt"
	"unsafe"

	"bluecode/runtime"
)

type Point struct{ X, Y int64 }

type Line struct{ Start, End Point }

func Example() {
	pod, err := bluecode.Load("testdata/geometry.aarch64.o", "testdata/geometry.json")
	if err != nil {
		panic(err)
	}
	defer pod.Close()

	size, _ := pod.Function("size")

	line := Line{Point{1, 2}, Point{4, 6}}
	var length uint64
	if _, err := size.Call(unsafe.Pointer(&line), unsafe.Pointer(&length), 1000); err != nil {
		panic(err)
	}

	fmt.Println(length)
	// Output: 7
}
