package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// MiniGameItem is one reward row or one bingo prize entry. The ID is the JSON
// object key; Count and Duration use the client's uint8/uint16 ranges.
type MiniGameItem struct {
	ID       int
	Name     string
	Count    int
	Duration int
}

// MiniGameItems decodes a JSON object as an ordered list. Go maps do not keep
// object order, but weapon-release row order is the client's slot index.
type MiniGameItems []MiniGameItem

func (m *MiniGameItems) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("mini game items must be an object")
	}
	var items []MiniGameItem
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return fmt.Errorf("mini game item key must be a string")
		}
		id, err := strconv.Atoi(name)
		if err != nil {
			return fmt.Errorf("invalid mini game item id %q", name)
		}
		var value struct {
			Name     string
			Count    int
			Duration int
		}
		if err = decoder.Decode(&value); err != nil {
			return err
		}
		if value.Count == 0 {
			value.Count = 1
		}
		items = append(items, MiniGameItem{
			ID:       id,
			Name:     value.Name,
			Count:    value.Count,
			Duration: value.Duration,
		})
	}
	if _, err = decoder.Token(); err != nil {
		return err
	}
	*m = items
	return nil
}

type MiniGameWeaponRelease struct {
	Active     bool
	Items      MiniGameItems
	Characters []string
}

type MiniGames struct {
	WeaponRelease MiniGameWeaponRelease
}
