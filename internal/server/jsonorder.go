package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

// jsonField is one member of a JSON object, kept in the order the file lists it.
// The reference iterates its parsed objects in file order, so decoding into a Go
// map (which is unordered) and sorting numerically sends the client a different
// metadata order than the baseline.
type jsonField struct {
	Key   string
	Value json.RawMessage
}

// readOrderedObject decodes a JSON object into its members, preserving order.
func readOrderedObject(data []byte) ([]jsonField, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("expected a JSON object")
	}
	var fields []jsonField
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("expected an object key")
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields = append(fields, jsonField{
			Key:   key,
			Value: value,
		})
	}
	if _, err = decoder.Token(); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, errors.New("unexpected trailing JSON content")
	}
	return fields, nil
}

// readOrderedFile loads a data JSON file and decodes it in file order. The
// shipped files carry comments and a BOM, so they go through the same stripper
// as config.ReadJSON.
func readOrderedFile(dir, name string) ([]jsonField, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	return readOrderedObject(config.StripComments(data))
}

// numericFields keeps the members whose key is a number, in file order. Files
// such as RandomWeaponList.json carry a non-numeric "Version" member first.
func numericFields(fields []jsonField) []jsonField {
	kept := make([]jsonField, 0, len(fields))
	for _, field := range fields {
		if _, err := strconv.Atoi(field.Key); err != nil {
			continue
		}
		kept = append(kept, field)
	}
	return kept
}

// member returns one member of an already-ordered object.
func member(fields []jsonField, key string) (json.RawMessage, bool) {
	for _, field := range fields {
		if field.Key == key {
			return field.Value, true
		}
	}
	return nil, false
}
