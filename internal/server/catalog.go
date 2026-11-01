package server

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type itemDefinition struct {
	Category, UseType, RewardID, Level int
	InGame                             bool
	ClassName                          string
}

func loadItems(dir string) (map[int]itemDefinition, error) {
	f, e := os.Open(filepath.Join(dir, "Data", "Item.csv"))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var header map[string]int
	result := map[int]itemDefinition{}
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if len(row) == 0 {
			continue
		}
		if header == nil {
			if strings.TrimSpace(strings.TrimPrefix(row[0], ";")) != "ID" {
				continue
			}
			header = map[string]int{}
			for i, name := range row {
				header[strings.TrimPrefix(name, ";")] = i
			}
			for _, name := range []string{"ID", "Category", "UseType", "InGameItem", "ClassName", "rewardID", "Level"} {
				if _, ok := header[name]; !ok {
					return nil, fmt.Errorf("Item.csv missing column %s", name)
				}
			}
			continue
		}
		value := func(key string) int {
			index := header[key]
			if index >= len(row) {
				return 0
			}
			v, _ := strconv.Atoi(row[index])
			return v
		}
		id := value("ID")
		if id <= 0 {
			continue
		}
		class := ""
		if index := header["ClassName"]; index < len(row) {
			class = row[index]
		}
		result[id] = itemDefinition{Category: value("Category"), UseType: value("UseType"), RewardID: value("rewardID"), Level: value("Level"), InGame: value("InGameItem") != 0, ClassName: class}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Item.csv has no items")
	}
	return result, nil
}
