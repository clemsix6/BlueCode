package bluecode

import (
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"runtime"
	"syscall"
)

// The compiler targets Linux for both CPUs; being freestanding, the same object runs on macOS.
var machines = map[string]elf.Machine{
	"arm64": elf.EM_AARCH64,
	"amd64": elf.EM_X86_64,
}

// object is what a pod file must be: one .text section, its symbols and the size of every
// function's frame, nothing else. No data, no relocations into the code, no imports, so the
// code can be copied anywhere and run as is.
type object struct {
	text     []byte
	entries  map[string]uint64 // offset in text of every global function
	maxFrame int               // the largest stack frame of any function
}

func parseObject(data []byte) (*object, error) {
	file, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("pod: %w", err)
	}

	if file.Type != elf.ET_REL {
		return nil, errors.New("pod: not a relocatable object")
	}

	if want := machines[runtime.GOARCH]; file.Machine != want {
		return nil, fmt.Errorf("pod: built for %s, this machine is %s", file.Machine, want)
	}

	var text, stackSizes *elf.Section
	var textIndex elf.SectionIndex

	for i, section := range file.Sections {
		switch section.Name {
		case ".text":
			text = section
			textIndex = elf.SectionIndex(i)
		case ".stack_sizes":
			stackSizes = section
		}
	}

	if text == nil || text.Size == 0 {
		return nil, errors.New("pod: no code")
	}

	if stackSizes == nil {
		return nil, errors.New("pod: no .stack_sizes section, compile with -fstack-size-section")
	}

	// The stack sizes come with relocations naming their functions; those never touch the code.
	for _, section := range file.Sections {
		switch {
		case (section.Type == elf.SHT_RELA || section.Type == elf.SHT_REL) && elf.SectionIndex(section.Info) == textIndex:
			return nil, fmt.Errorf("pod: has relocations in the code (%s)", section.Name)
		case section.Flags&elf.SHF_ALLOC != 0 && section.Size > 0 && section != text:
			return nil, fmt.Errorf("pod: has a data section (%s)", section.Name)
		}
	}

	code, err := text.Data()
	if err != nil {
		return nil, fmt.Errorf("pod: %w", err)
	}

	sizes, err := stackSizes.Data()
	if err != nil {
		return nil, fmt.Errorf("pod: %w", err)
	}

	maxFrame, err := largestFrame(sizes)
	if err != nil {
		return nil, err
	}

	symbols, err := file.Symbols()
	if err != nil {
		return nil, fmt.Errorf("pod: %w", err)
	}

	entries := make(map[string]uint64)
	for _, symbol := range symbols {
		if symbol.Section == textIndex && elf.ST_BIND(symbol.Info) == elf.STB_GLOBAL {
			entries[symbol.Name] = symbol.Value
		}
	}

	return &object{code, entries, maxFrame}, nil
}

// largestFrame reads a .stack_sizes section: one entry per function, an 8-byte address then
// the frame size as an unsigned LEB128. Only the largest size matters here.
func largestFrame(sizes []byte) (int, error) {
	largest := 0

	for i := 0; i < len(sizes); {
		i += 8

		size, shift := 0, 0
		for {
			if i >= len(sizes) {
				return 0, errors.New("pod: truncated .stack_sizes")
			}
			b := sizes[i]
			i++
			size |= int(b&0x7f) << shift
			shift += 7
			if b < 0x80 {
				break
			}
		}

		largest = max(largest, size)
	}

	return largest, nil
}

// mapExecutable copies the code into a mapping of its own and makes it executable. The mapping
// is written once, before it can run, and never again.
func mapExecutable(code []byte) ([]byte, error) {
	memory, err := syscall.Mmap(-1, 0, len(code), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("pod: mmap: %w", err)
	}

	copy(memory, code)

	if err := syscall.Mprotect(memory, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		syscall.Munmap(memory)
		return nil, fmt.Errorf("pod: mprotect: %w", err)
	}

	start := addressOf(memory)
	flushInstructionCache(start, start+uintptr(len(memory)))

	return memory, nil
}
