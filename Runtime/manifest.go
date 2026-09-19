package bluecode

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Manifest is what the compiler emits next to a pod: the depth limit its code enforces, the
// errors its code can fail with, the memory layout of every struct, and for every function the
// symbol of its entry and the layout of its argument and result blocks.
// Offsets follow the natural alignment rule that LLVM, C and Go share, so a Go struct with
// the same fields in the same order is laid out identically and can be passed as is.
// Functions are in the order of their numbers, which the frames of a trace refer to.
type Manifest struct {
	Source    string           `json:"source"`   // the file the pod was compiled from, for the lines of a trace
	MaxDepth  int              `json:"maxDepth"` // how deep the code lets calls nest before giving up
	Errors    []ErrorName      `json:"errors"`
	Structs   map[string]Block `json:"structs"`
	State     Block            `json:"state"` // the variables the pod keeps between calls, as one block
	Functions []Signature      `json:"functions"`
}

// ErrorName is an error the pod declares, with the number its code fails with.
type ErrorName struct {
	Name string `json:"name"`
	Code int64  `json:"code"`
}

// Signature describes one function. Only an External one can be called: it has the Symbol of
// its entry, Args, the block its parameters form, and Results, the block its return values are
// written to. A function that Fails ends its results with an "error" field holding the number
// of the error it failed with, zero on success. An internal function is listed by name only,
// so that the frames of a trace can be read.
type Signature struct {
	Name     string `json:"name"`
	External bool   `json:"external"`
	Symbol   string `json:"symbol"`
	Fails    bool   `json:"fails"`
	Args     Block  `json:"args"`
	Results  Block  `json:"results"`
}

// Block is a run of values laid out one after the other, padding included in Size.
type Block struct {
	Size   int     `json:"size"`
	Fields []Field `json:"fields"`
}

// Field is one value of a block. Type is "int", "uint", "bool", "error" or the name of a
// struct. Results have no name. Ref marks an argument the function works on in place: after
// the call, the caller's block holds what the function left there.
type Field struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Ref    bool   `json:"ref"`
}

func parseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}

	return &manifest, nil
}

// Canonical checks that data is the one valid representation of a block: exactly its size,
// every bool 0 or 1, every padding byte 0. A node runs this on the bytes of a transaction
// before handing them to a pod, so that a value has a single encoding and a single hash.
func (m *Manifest) Canonical(block Block, data []byte) error {
	if len(data) != block.Size {
		return fmt.Errorf("block is %d bytes, expected %d", len(data), block.Size)
	}

	covered := make([]bool, len(data))
	if err := m.walk(block, data, 0, covered); err != nil {
		return err
	}

	for i, isCovered := range covered {
		if !isCovered && data[i] != 0 {
			return fmt.Errorf("padding byte %d is not zero", i)
		}
	}

	return nil
}

func (m *Manifest) walk(block Block, data []byte, base int, covered []bool) error {
	for _, field := range block.Fields {
		at := base + field.Offset

		if strings.HasPrefix(field.Type, "own ") {
			return fmt.Errorf("field %q owns memory of the pod: it never crosses to the host", field.Name)
		}

		switch field.Type {
		case "int", "uint", "error":
			for i := at; i < at+8; i++ {
				covered[i] = true
			}

		case "bool":
			if data[at] > 1 {
				return fmt.Errorf("byte %d is %d, not a bool", at, data[at])
			}
			covered[at] = true

		default:
			inner, ok := m.Structs[field.Type]
			if !ok {
				return fmt.Errorf("unknown type %q", field.Type)
			}
			if err := m.walk(inner, data, at, covered); err != nil {
				return err
			}
		}
	}

	return nil
}
