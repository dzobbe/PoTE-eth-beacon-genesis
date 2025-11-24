package beaconutils

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/ethpandaops/eth-beacon-genesis/beaconconfig"
	"github.com/sirupsen/logrus"
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
	// hardcodedTEEQuote is a hardcoded 8192-byte string used to populate genesis
	// headers. The quote is always set to this fixed value.
	hardcodedTEEQuote = make([]byte, 8192)

	// defaultTEEType identifies the placeholder TEE vendor used when no
	// configuration override is present. The value is intentionally fixed so
	// that downstream clients can rely on deterministic genesis metadata even
	// before configuration plumbing is added.
	defaultTEEType = TEETypeSEV

	teeTypeField  = "ProposerTEEType"
	teeQuoteField = "ProposerTEEQuote"

	teeTypeLookup = map[string]TEEType{
		"sev": defaultTEEType,
		"tdx": TEETypeTDX,
		"cca": TEETypeCCA,
	}
)

func init() {
	// Initialize hardcoded quote with a fixed 8192-byte string
	// Fill with a repeating pattern for deterministic output
	chunk := []byte("PoTE-genesis-TEE")
	repeat := (8192 + len(chunk) - 1) / len(chunk)
	buf := bytes.Repeat(chunk, repeat)
	copy(hardcodedTEEQuote, buf[:8192])
}

// ApplyDefaultTEEToHeader populates the proposer TEE fields on a beacon block
// header if the build includes the extended metadata. Older builds that do not
// expose these fields are left untouched.
func ApplyDefaultTEEToHeader(header interface{}) {
	applyTEEToHeader(header, defaultTEEType, hardcodedTEEQuote)
}

// GetGenesisProposerTEEFields resolves the proposer TEE metadata that should be embedded in the
// genesis block header. It prefers vendor type from mnemonics.yml (TEE_VENDOR_FROM_MNEMONICS),
// then a dedicated TEE_PROPOSER_VENDOR override, and falls back to the global TEE_VENDOR default.
// The quote is always hardcoded to an 8192-byte string.
func GetGenesisProposerTEEFields(cfg *beaconconfig.Config) (TEEType, []byte, error) {
	const teeVendorMin = 0
	const teeVendorMax = 2
	const teeQuoteSize = 8192

	// Quote is always hardcoded to 8192 bytes
	quoteBytes := make([]byte, teeQuoteSize)
	copy(quoteBytes, hardcodedTEEQuote)

	// First, try to get vendor type from mnemonics.yml
	var proposerVendor uint64
	var found bool

	if vendorTypeStr, ok := cfg.GetString("TEE_VENDOR_FROM_MNEMONICS"); ok && vendorTypeStr != "" {
		// Convert vendor type string to TEEType
		if teeType, ok := TEETypeFromString(vendorTypeStr); ok {
			proposerVendor = uint64(teeType)
			found = true
			logrus.Infof("using vendor type from mnemonics: %s (TEEType: %d)", vendorTypeStr, proposerVendor)
		} else {
			logrus.Warnf("invalid vendor type from mnemonics: %s (not a valid TEEType)", vendorTypeStr)
		}
	}

	// If not found from mnemonics, try TEE_PROPOSER_VENDOR
	if !found {
		defaultVendor := cfg.GetUintDefault("TEE_VENDOR", uint64(teeVendorMin))
		if defaultVendor < teeVendorMin || defaultVendor > teeVendorMax {
			return 0, quoteBytes, fmt.Errorf("invalid TEE_VENDOR value: %d (must be between %d and %d)", defaultVendor, teeVendorMin, teeVendorMax)
		}

		proposerVendor = cfg.GetUintDefault("TEE_PROPOSER_VENDOR", defaultVendor)
		if proposerVendor < teeVendorMin || proposerVendor > teeVendorMax {
			return 0, quoteBytes, fmt.Errorf("invalid TEE_PROPOSER_VENDOR value: %d (must be between %d and %d)", proposerVendor, teeVendorMin, teeVendorMax)
		}
		if proposerVendor == defaultVendor {
			logrus.Infof("using default vendor type from TEE_VENDOR config: %d", proposerVendor)
		} else {
			logrus.Infof("using vendor type from TEE_PROPOSER_VENDOR config: %d", proposerVendor)
		}
	}

	return TEEType(proposerVendor), quoteBytes, nil
}

// ApplyTEEToHeaderFromConfig populates the proposer TEE fields on a beacon block header
// using configuration values. Always applies TEE info with hardcoded quote and vendor type
// from mnemonics.yml (if available) or config. Falls back to defaults if config values are not available.
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

	// Always apply TEE info to header
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
