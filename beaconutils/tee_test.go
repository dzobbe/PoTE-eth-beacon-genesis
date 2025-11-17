package beaconutils

import "testing"

type (
	testHeader struct {
		ProposerTEEType  uint8
		ProposerTEEQuote [8192]byte
	}

	testHeaderSlice struct {
		ProposerTEEType  uint8
		ProposerTEEQuote []byte
	}
)

func TestApplyDefaultTEEToHeader_Array(t *testing.T) {
	header := &testHeader{}

	ApplyDefaultTEEToHeader(header)

	if header.ProposerTEEType != uint8(TEETypeSEV) {
		t.Fatalf("unexpected tee type: got %d want %d", header.ProposerTEEType, TEETypeSEV)
	}

	if header.ProposerTEEQuote[0] != hardcodedTEEQuote[0] || header.ProposerTEEQuote[8191] != hardcodedTEEQuote[8191] {
		t.Fatalf("quote was not populated with defaults")
	}
}

func TestApplyDefaultTEEToHeader_Slice(t *testing.T) {
	header := &testHeaderSlice{}

	ApplyDefaultTEEToHeader(header)

	if header.ProposerTEEType != uint8(TEETypeSEV) {
		t.Fatalf("unexpected tee type: got %d want %d", header.ProposerTEEType, TEETypeSEV)
	}

	if len(header.ProposerTEEQuote) != len(hardcodedTEEQuote) {
		t.Fatalf("unexpected tee quote length: got %d want %d", len(header.ProposerTEEQuote), len(hardcodedTEEQuote))
	}
}

func TestApplyDefaultTEEToHeader_NoFields(t *testing.T) {
	type minimalHeader struct {
		Slot uint64
	}

	header := &minimalHeader{}
	ApplyDefaultTEEToHeader(header)
	// ensure there is no panic and header untouched.
	if header.Slot != 0 {
		t.Fatalf("unexpected mutation of unrelated fields")
	}
}

func TestTEETypeFromString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  TEEType
		shouldErr bool
	}{
		{name: "sev uppercase", input: "SEV", expected: TEETypeSEV},
		{name: "tdx lowercase", input: "tdx", expected: TEETypeTDX},
		{name: "cca mixed case", input: "CcA", expected: TEETypeCCA},
		{name: "unknown", input: "sgx", shouldErr: true},
		{name: "whitespace", input: "  sev  ", expected: TEETypeSEV},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := TEETypeFromString(tt.input)
			if tt.shouldErr {
				if ok {
					t.Fatalf("expected parse failure for %q", tt.input)
				}

				return
			}

			if !ok {
				t.Fatalf("expected parse success for %q", tt.input)
			}

			if got != tt.expected {
				t.Fatalf("unexpected tee type: got %d want %d", got, tt.expected)
			}
		})
	}
}
