package node

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// decodeStrictJSON decodes one JSON value, rejecting duplicate decoded keys at
// every object depth, non-canonical field aliases, unknown struct fields,
// invalid UTF-8/unpaired surrogates, malformed syntax, and trailing values.
func decodeStrictJSON(encoded []byte, destination any) error {
	if err := scanJSONKeys(encoded, true); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(encoded []byte) error {
	return scanJSONKeys(encoded, false)
}

func scanJSONKeys(encoded []byte, requireLowercaseKeys bool) error {
	if !utf8.Valid(encoded) {
		return errors.New("invalid UTF-8 in JSON")
	}
	if err := validateJSONUnicodeEscapes(encoded); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder, requireLowercaseKeys); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

// validateJSONUnicodeEscapes rejects unpaired UTF-16 surrogates before
// encoding/json can silently replace them with U+FFFD.
func validateJSONUnicodeEscapes(encoded []byte) error {
	inString := false
	for index := 0; index < len(encoded); index++ {
		if !inString {
			if encoded[index] == '"' {
				inString = true
			}
			continue
		}
		switch encoded[index] {
		case '"':
			inString = false
		case '\\':
			if index+1 >= len(encoded) || encoded[index+1] != 'u' {
				index++
				continue
			}
			codeUnit, end, ok := parseJSONUnicodeEscape(encoded, index)
			if !ok {
				return nil // encoding/json reports malformed escape syntax.
			}
			switch {
			case codeUnit >= 0xD800 && codeUnit <= 0xDBFF:
				lowSurrogate, pairEnd, ok := parseJSONUnicodeEscape(encoded, end)
				if !ok || lowSurrogate < 0xDC00 || lowSurrogate > 0xDFFF {
					return errors.New("unpaired UTF-16 surrogate escape in JSON")
				}
				index = pairEnd - 1
			case codeUnit >= 0xDC00 && codeUnit <= 0xDFFF:
				return errors.New("unpaired UTF-16 surrogate escape in JSON")
			default:
				index = end - 1
			}
		}
	}
	return nil
}

func parseJSONUnicodeEscape(encoded []byte, start int) (uint16, int, bool) {
	if start < 0 || start+6 > len(encoded) || encoded[start] != '\\' || encoded[start+1] != 'u' {
		return 0, 0, false
	}
	var value uint16
	for _, digit := range encoded[start+2 : start+6] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, 0, false
		}
	}
	return value, start + 6, true
}

func consumeUniqueJSONValue(decoder *json.Decoder, requireLowercaseKeys bool) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate JSON object key")
			}
			if requireLowercaseKeys && key != strings.ToLower(key) {
				return errors.New("non-canonical JSON object key")
			}
			seen[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder, requireLowercaseKeys); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder, requireLowercaseKeys); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}
