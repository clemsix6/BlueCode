package harness

import (
	"debug/elf"
	"fmt"
)

// Verify checks what a pod must be: one section of code, no relocation into it and nothing else
// loadable, so the same bytes can be copied anywhere and run as they are. It reads the object
// itself rather than asking the runtime, so the two can disagree and the test say so.
func Verify(object string) error {
	file, err := elf.Open(object)
	if err != nil {
		return fmt.Errorf("pod %s: %w", object, err)
	}
	defer file.Close()

	if file.Type != elf.ET_REL {
		return fmt.Errorf("pod %s: not a relocatable object", object)
	}

	text, index := section(file, ".text")
	if text == nil || text.Size == 0 {
		return fmt.Errorf("pod %s: no code", object)
	}

	if sizes, _ := section(file, ".stack_sizes"); sizes == nil {
		return fmt.Errorf("pod %s: no .stack_sizes section, the runtime cannot size a stack for it", object)
	}

	return verifySections(file, object, text, index)
}

// section finds a section by name and returns it with its index, which relocations point to.
func section(file *elf.File, name string) (*elf.Section, elf.SectionIndex) {
	for i, candidate := range file.Sections {
		if candidate.Name == name {
			return candidate, elf.SectionIndex(i)
		}
	}

	return nil, 0
}

// verifySections rejects a relocation targeting the code and any other section the loader would
// have to map: either one means the pod needs a linker, which there is none of.
func verifySections(file *elf.File, object string, text *elf.Section, index elf.SectionIndex) error {
	for _, candidate := range file.Sections {
		relocation := candidate.Type == elf.SHT_REL || candidate.Type == elf.SHT_RELA

		switch {
		case relocation && elf.SectionIndex(candidate.Info) == index:
			return fmt.Errorf("pod %s: has relocations in the code (%s)", object, candidate.Name)

		case candidate.Flags&elf.SHF_ALLOC != 0 && candidate.Size > 0 && candidate != text:
			return fmt.Errorf("pod %s: has a data section (%s)", object, candidate.Name)
		}
	}

	return nil
}
