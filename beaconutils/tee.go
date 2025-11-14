package beaconutils

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"

	"github.com/ethpandaops/eth-beacon-genesis/beaconconfig"
)

// TEEType enumerates the supported TEE vendor encodings used by the execution
// payload header metadata. The numeric assignments follow the ordering used in
// poc-lighthouse so the default genesis state stays aligned with the consensus
// client.
type TEEType byte

const (
	// TEETypeSEV represents AMD SEV.
	TEETypeSEV TEEType = iota
	// TEETypeTDX represents Intel TDX.
	TEETypeTDX
	// TEETypeCCA represents ARM CCA.
	TEETypeCCA
)

var (
	// teeQuoteChunk is repeated to synthesise the fixed-size attestation blob.
	teeQuoteChunk = "PoTE-genesis-TEE"

	// defaultTEEType identifies the placeholder TEE vendor used when no
	// configuration override is present. The value is intentionally fixed so
	// that downstream clients can rely on deterministic genesis metadata even
	// before configuration plumbing is added.
	defaultTEEType = TEETypeSEV

	// defaultTEEQuote is a deterministic 8 KiB payload used to populate genesis
	// headers. The quote contents do not aim to be a valid attestation; they
	// simply exercise the serialization paths introduced for TEE metadata.
	defaultTEEQuote = makeTEEQuote()
	teeTypeField    = "ProposerTEEType"
	teeQuoteField   = "ProposerTEEQuote"

	teeTypeLookup = map[string]TEEType{
		"sev": defaultTEEType,
		"tdx": TEETypeTDX,
		"cca": TEETypeCCA,
	}
)

func makeTEEQuote() []byte {
	chunk := []byte(teeQuoteChunk)
	repeat := (8192 + len(chunk) - 1) / len(chunk)
	buf := bytes.Repeat(chunk, repeat)
	return buf[:8192]
}

// ApplyDefaultTEEToHeader populates the proposer TEE fields on a beacon block
// header if the build includes the extended metadata. Older builds that do not
// expose these fields are left untouched.
func ApplyDefaultTEEToHeader(header interface{}) {
	applyTEEToHeader(header, defaultTEEType, defaultTEEQuote)
}

// GetGenesisProposerTEEFields resolves the proposer TEE metadata that should be embedded in the
// genesis block header. It prefers a dedicated TEE_PROPOSER_VENDOR override and falls back to the
// global TEE_VENDOR default that is already used for validators. Optional attestation quote data can be
// supplied via TEE_PROPOSER_ATTESTATION as a base64-encoded string; legacy byte inputs are converted to
// base64 automatically.
func GetGenesisProposerTEEFields(cfg *beaconconfig.Config) (TEEType, []byte, error) {
	const teeVendorMin = 0
	const teeVendorMax = 2
	const teeQuoteSize = 8192

	emptyQuote := make([]byte, teeQuoteSize)

	defaultVendor := cfg.GetUintDefault("TEE_VENDOR", uint64(teeVendorMin))
	if defaultVendor < teeVendorMin || defaultVendor > teeVendorMax {
		return 0, emptyQuote, fmt.Errorf("invalid TEE_VENDOR value: %d (must be between %d and %d)", defaultVendor, teeVendorMin, teeVendorMax)
	}

	proposerVendor := cfg.GetUintDefault("TEE_PROPOSER_VENDOR", defaultVendor)
	if proposerVendor < teeVendorMin || proposerVendor > teeVendorMax {
		return 0, emptyQuote, fmt.Errorf("invalid TEE_PROPOSER_VENDOR value: %d (must be between %d and %d)", proposerVendor, teeVendorMin, teeVendorMax)
	}

	var quoteString string
	if str, ok := cfg.GetString("TEE_PROPOSER_ATTESTATION"); ok {
		quoteString = str
	} else if bytes, ok := cfg.GetBytes("TEE_PROPOSER_ATTESTATION"); ok {
		quoteString = base64.StdEncoding.EncodeToString(bytes)
	}

	quoteBytes := make([]byte, teeQuoteSize)
	if quoteString != "" {
		decoded, err := base64.StdEncoding.DecodeString(quoteString)
		if err != nil {
			return 0, emptyQuote, fmt.Errorf("TEE_PROPOSER_ATTESTATION invalid base64: %w", err)
		}
		if len(decoded) != teeQuoteSize {
			return 0, emptyQuote, fmt.Errorf(
				"TEE_PROPOSER_ATTESTATION decoded length mismatch: got %d bytes, expected %d",
				len(decoded),
				teeQuoteSize,
			)
		}
		copy(quoteBytes, decoded)
	} else {
		// Use default quote if none provided
		copy(quoteBytes, defaultTEEQuote)
	}

	return TEEType(proposerVendor), quoteBytes, nil
}

// ApplyTEEToHeaderFromConfig populates the proposer TEE fields on a beacon block header
// using configuration values. Falls back to defaults if config values are not available.
// This should be used instead of ApplyDefaultTEEToHeader when config is available.
func ApplyTEEToHeaderFromConfig(header interface{}, cfg *beaconconfig.Config) {
	if cfg == nil {
		// Fallback to defaults if no config provided
		ApplyDefaultTEEToHeader(header)
		return
	}

	teeType, teeQuote, err := GetGenesisProposerTEEFields(cfg)
	if err != nil {
		// Log error but fallback to defaults
		// Note: In production, you might want to return the error instead
		ApplyDefaultTEEToHeader(header)
		return
	}

	applyTEEToHeader(header, teeType, teeQuote)
}

// TEETypeFromString converts a human-readable vendor identifier (case
// insensitive) to the matching TEEType. Unknown identifiers return false.
func TEETypeFromString(name string) (TEEType, bool) {
	teeType, found := teeTypeLookup[strings.ToLower(strings.TrimSpace(name))]
	return teeType, found
}

func applyTEEToHeader(header interface{}, teeType TEEType, teeQuote []byte) {
	if header == nil {
		return
	}

	v := reflect.ValueOf(header)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}

	elem := v.Elem()
	if !elem.IsValid() {
		return
	}

	applyTEEType(elem.FieldByName(teeTypeField), teeType)
	applyTEEQuote(elem.FieldByName(teeQuoteField), teeQuote)
}

func applyTEEType(field reflect.Value, teeType TEEType) {
	if !field.IsValid() || !field.CanSet() {
		return
	}

	value := uint64(teeType)

	switch field.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint, reflect.Uintptr:
		field.SetUint(value)
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		field.SetInt(int64(value))
	}
}

func applyTEEQuote(field reflect.Value, teeQuote []byte) {
	if !field.IsValid() || !field.CanSet() {
		return
	}

	switch field.Kind() {
	case reflect.Array:
		writeArray(field, teeQuote)
	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.Uint8 {
			tmp := make([]byte, len(teeQuote))
			copy(tmp, teeQuote)
			field.SetBytes(tmp)
		}
	}
}

func writeArray(field reflect.Value, teeQuote []byte) {
	length := field.Len()
	for i := 0; i < length; i++ {
		var value byte
		if i < len(teeQuote) {
			value = teeQuote[i]
		}
		elem := field.Index(i)
		if !elem.CanSet() {
			continue
		}
		switch elem.Kind() {
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint, reflect.Uintptr:
			elem.SetUint(uint64(value))
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			elem.SetInt(int64(value))
		}
	}
}
