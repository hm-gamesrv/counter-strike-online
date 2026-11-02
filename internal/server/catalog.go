package server

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

type itemDefinition struct {
	Category   int
	UseType    int
	RewardID   int
	Level      int
	InGame     bool
	ClassName  string
	Resource   string
	ZombieSkin int
	Grade      int
}

// itemBoxAward is one rolled decoder prize. Duration follows the client
// convention: -1 is permanent, otherwise it is a number of days.
type itemBoxAward struct {
	ID       int
	Duration int
	Grade    int
}

type itemBoxRate struct {
	Rate      float64
	Grade     int
	Durations []int
	Items     []int
}

type itemBox struct {
	ID    int
	Total float64
	Rates []itemBoxRate
}

// loadItemBoxes reads the same ItemBox.json used by the upstream lucky item
// manager. Rates are stored sorted and normalized at roll time, matching the
// client data rather than assuming they add up to exactly 100.
func loadItemBoxes(dir string) (map[int]*itemBox, error) {
	var raw map[string]json.RawMessage
	if e := config.ReadJSON(filepath.Join(dir, "ItemBox.json"), &raw); e != nil {
		return nil, e
	}
	delete(raw, "Version")
	boxes := map[int]*itemBox{}
	for key, value := range raw {
		id, e := strconv.Atoi(key)
		if e != nil || id <= 0 || id > 65535 {
			return nil, fmt.Errorf("invalid item box id %q", key)
		}
		var cfg struct {
			Rate map[string]struct {
				Grade    int
				Duration []int
				Items    []int
			}
		}
		if e = json.Unmarshal(value, &cfg); e != nil {
			return nil, fmt.Errorf("item box %d: %w", id, e)
		}
		box := &itemBox{ID: id}
		keys := make([]string, 0, len(cfg.Rate))
		for k := range cfg.Rate {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, _ := strconv.ParseFloat(keys[i], 64)
			b, _ := strconv.ParseFloat(keys[j], 64)
			return a < b
		})
		for _, k := range keys {
			rate, e := strconv.ParseFloat(k, 64)
			if e != nil || rate <= 0 {
				return nil, fmt.Errorf("item box %d has invalid rate %q", id, k)
			}
			entry := cfg.Rate[k]
			if len(entry.Items) == 0 || len(entry.Duration) == 0 {
				return nil, fmt.Errorf("item box %d rate %s has no items or durations", id, k)
			}
			for _, item := range entry.Items {
				if item <= 0 || item > 65535 {
					return nil, fmt.Errorf("item box %d rate %s has invalid item %d", id, k, item)
				}
			}
			for _, duration := range entry.Duration {
				if duration < -1 || duration > 36500 {
					return nil, fmt.Errorf("item box %d rate %s has invalid duration %d", id, k, duration)
				}
			}
			box.Rates = append(box.Rates, itemBoxRate{Rate: rate, Grade: entry.Grade, Durations: entry.Duration, Items: entry.Items})
			box.Total += rate
		}
		if len(box.Rates) == 0 || box.Total <= 0 {
			return nil, fmt.Errorf("item box %d has no rates", id)
		}
		boxes[id] = box
	}
	return boxes, nil
}

func (b *itemBox) roll() (itemBoxAward, error) {
	x := rand.Float64()
	var cumulative float64
	for _, rate := range b.Rates {
		cumulative += rate.Rate / b.Total
		if x <= cumulative {
			return itemBoxAward{
				ID:       rate.Items[rand.IntN(len(rate.Items))],
				Duration: rate.Durations[rand.IntN(len(rate.Durations))],
				Grade:    rate.Grade,
			}, nil
		}
	}
	return itemBoxAward{}, errors.New("item box rates do not cover the random range")
}

func loadWeaponPaints(dir string) (map[int][]int, error) {
	var raw map[string]json.RawMessage
	if e := config.ReadJSON(filepath.Join(dir, "WeaponPaints.json"), &raw); e != nil {
		return nil, e
	}
	delete(raw, "Version")
	paints := map[int][]int{}
	for key, value := range raw {
		id, e := strconv.Atoi(key)
		if e != nil || id <= 0 || id > 65535 {
			return nil, fmt.Errorf("invalid weapon paint id %q", key)
		}
		var cfg struct {
			Paints []int
		}
		if e = json.Unmarshal(value, &cfg); e != nil {
			return nil, fmt.Errorf("weapon paint %d: %w", id, e)
		}
		for _, paint := range cfg.Paints {
			if paint <= 0 || paint > 65535 {
				return nil, fmt.Errorf("weapon paint %d has invalid paint %d", id, paint)
			}
		}
		paints[id] = cfg.Paints
	}
	return paints, nil
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
			for _, name := range []string{"ID", "Category", "UseType", "InGameItem", "ClassName", "rewardID", "Level", "recourcename"} {
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
		class, resource := "", ""
		if index := header["ClassName"]; index < len(row) {
			class = row[index]
		}
		if index := header["recourcename"]; index < len(row) {
			resource = row[index]
		}
		result[id] = itemDefinition{Category: value("Category"), UseType: value("UseType"), RewardID: value("rewardID"), Level: value("Level"), InGame: value("InGameItem") != 0, ClassName: class, Resource: resource, ZombieSkin: value("ZombieSkin"), Grade: value("ItemGrade")}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Item.csv has no items")
	}
	return result, nil
}
