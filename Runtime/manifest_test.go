package bluecode

import "testing"

// transferManifest mirrors the structs and the "transfer" signature of bank.bc, trimmed to the
// fields Canonical reads: sizes, offsets, types and the ref flag.
const transferManifest = `{
	"structs": {
		"Account": {
			"size": 24,
			"fields": [
				{"name": "id", "type": "uint", "offset": 0},
				{"name": "balance", "type": "uint", "offset": 8},
				{"name": "frozen", "type": "bool", "offset": 16}
			]
		},
		"Fees": {
			"size": 16,
			"fields": [
				{"name": "flat", "type": "uint", "offset": 0},
				{"name": "per_thousand", "type": "uint", "offset": 8}
			]
		}
	},
	"functions": [
		{
			"name": "transfer",
			"args": {
				"size": 72,
				"fields": [
					{"name": "from", "type": "Account", "offset": 0, "ref": true},
					{"name": "to", "type": "Account", "offset": 24, "ref": true},
					{"name": "amount", "type": "uint", "offset": 48},
					{"name": "fees", "type": "Fees", "offset": 56}
				]
			}
		}
	]
}`

func TestCanonical(t *testing.T) {
	manifest, err := parseManifest([]byte(transferManifest))
	if err != nil {
		t.Fatal(err)
	}
	block := manifest.Functions[0].Args

	data := make([]byte, block.Size)
	if err := manifest.Canonical(block, data); err != nil {
		t.Errorf("a zeroed struct should be canonical: %v", err)
	}

	data[16] = 2 // from.frozen
	if err := manifest.Canonical(block, data); err == nil {
		t.Error("a bool of 2 should be rejected")
	}

	data[16] = 0
	data[17] = 1 // padding after from.frozen
	if err := manifest.Canonical(block, data); err == nil {
		t.Error("non-zero padding should be rejected")
	}

	if err := manifest.Canonical(block, data[:10]); err == nil {
		t.Error("a short block should be rejected")
	}
}
