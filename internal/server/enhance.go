package server

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

type enhanceLimit struct {
	TotalMaxLv int
	Caps       [7]int
}

var enhanceItemExp = [7]int{0, 10, 20, 50, 100, 200, 500}

func readEnhanceCSV(dir, name string, headerCheck string) ([][]string, map[string]int, error) {
	f, err := os.Open(filepath.Join(dir, "Data", name))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var header map[string]int
	var rows [][]string
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if len(row) == 0 {
			continue
		}
		if header == nil {
			name := strings.TrimSpace(strings.TrimPrefix(row[0], ";"))
			if name != headerCheck {
				continue
			}
			header = map[string]int{}
			for i, cell := range row {
				header[strings.TrimSpace(strings.TrimPrefix(cell, ";"))] = i
			}
			continue
		}
		rows = append(rows, row)
	}
	if header == nil {
		return nil, nil, fmt.Errorf("%s: missing header", name)
	}
	return rows, header, nil
}

func loadReinforceExp(dir string) (map[int][]int, error) {
	f, err := os.Open(filepath.Join(dir, "Data", "ReinforceMaxEXP.csv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.Comment = ';'
	exp := map[int][]int{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(row) < 8 {
			return nil, errors.New("ReinforceMaxEXP.csv: row too short")
		}
		level, err := strconv.Atoi(strings.TrimSpace(row[0]))
		if err != nil || level < 0 || level > 9 {
			return nil, errors.New("ReinforceMaxEXP.csv: invalid level")
		}
		grades := make([]int, 7)
		for i := range grades {
			value, err := strconv.Atoi(strings.TrimSpace(row[i+1]))
			if err != nil || value < 0 || value > 1000000000 {
				return nil, errors.New("ReinforceMaxEXP.csv: invalid exp value")
			}
			grades[i] = value
		}
		exp[level] = grades
	}
	if len(exp) == 0 {
		return nil, errors.New("ReinforceMaxEXP.csv has no rows")
	}
	return exp, nil
}

func loadReinforceMax(dir string) (map[int]enhanceLimit, error) {
	rows, header, err := readEnhanceCSV(dir, "ReinforceMaxLv.csv", "Id")
	if err != nil {
		return nil, err
	}
	for _, column := range []string{"Id", "TotalMaxLv", "Damage", "Accuracy", "KickBack", "Weight", "FireRate", "Ammo"} {
		if _, ok := header[column]; !ok {
			return nil, fmt.Errorf("ReinforceMaxLv.csv missing column %s", column)
		}
	}
	limits := map[int]enhanceLimit{}
	for _, row := range rows {
		cell := func(column string) int {
			index, ok := header[column]
			if !ok || index >= len(row) {
				return 0
			}
			value, _ := strconv.Atoi(strings.TrimSpace(row[index]))
			return value
		}
		id := cell("Id")
		if id <= 0 || id > 65535 {
			return nil, errors.New("ReinforceMaxLv.csv: invalid item id")
		}
		limit := enhanceLimit{TotalMaxLv: cell("TotalMaxLv")}
		if limit.TotalMaxLv < 1 || limit.TotalMaxLv > 9 {
			return nil, fmt.Errorf("ReinforceMaxLv.csv: item %d has invalid max level", id)
		}
		columns := []string{"Damage", "Accuracy", "KickBack", "Weight", "FireRate", "Ammo", "9over_dmg"}
		for i, column := range columns {
			limit.Caps[i] = cell(column)
			maxCap := 15
			if i == 6 {
				// 9over_dmg is a damage value, not a nibble-packed level.
				maxCap = 255
			}
			if limit.Caps[i] < 0 || limit.Caps[i] > maxCap {
				return nil, fmt.Errorf("ReinforceMaxLv.csv: item %d has invalid %s cap", id, column)
			}
		}
		limits[id] = limit
	}
	if len(limits) == 0 {
		return nil, errors.New("ReinforceMaxLv.csv has no rows")
	}
	return limits, nil
}

func unpackEnhanceAttributes(value int) [7]int {
	var attrs [7]int
	for i := range attrs {
		attrs[i] = value >> (4 * i) & 0xF
	}
	return attrs
}

func packEnhanceAttributes(attrs [7]int) int {
	value := 0
	for i, level := range attrs {
		value |= level << (4 * i)
	}
	return value
}

func enhanceLevel(attrs [7]int) int {
	level := 0
	for i, value := range attrs {
		if i == 6 && value != 0 {
			level++
			continue
		}
		level += value
	}
	return level
}

func (s *Server) enhanceAttribute(attrs [7]int, currentLevel int, caps [7]int) (int, bool) {
	if currentLevel == 8 && caps[6] > attrs[6] {
		return 6, true
	}
	for attempts := 0; attempts < 64; attempts++ {
		index := rand.IntN(5)
		if index == 0 {
			index = 1
		} else if index == 1 {
			index = 0
		}
		if caps[index] > attrs[index] {
			return index, true
		}
	}
	for index := 0; index < 7; index++ {
		if caps[index] > attrs[index] {
			return index, true
		}
	}
	return 0, false
}

func (s *Server) enhanceError(c *client) {
	c.send(78, &protocol.Writer{Data: []byte{14, 5}})
}

func (s *Server) enhanceReply(c *client, status, gameSlot, level, attribute int) {
	w := &protocol.Writer{}
	w.U8(14)
	w.U8(status)
	if status == 0 {
		w.U16(gameSlot)
		w.U16(level)
		w.U32(level)
		w.U8(attribute)
		w.U8(0)
	} else if status == 1 {
		w.U8(0)
	}
	c.send(78, w)
}

func (s *Server) enhanceRequest(c *client, p *protocol.Reader) error {
	if p.U8() != 0 {
		s.enhanceError(c)
		return nil
	}
	p.U8()
	targetGameSlot := p.U16()
	count := p.U8()
	materialGameSlots := make([]int, 0, count)
	for i := 0; i < count; i++ {
		p.U8()
		materialGameSlots = append(materialGameSlots, p.U16())
	}
	if p.Err != nil {
		return p.Err
	}
	if p.Pos != len(p.Data) || count > 32 {
		s.enhanceError(c)
		return nil
	}
	defaults := len(s.Config.DefaultUser.DefaultItems)
	targetSlot := targetGameSlot - defaults
	if targetSlot <= 0 || len(materialGameSlots) == 0 {
		s.enhanceError(c)
		return nil
	}
	items, err := s.Store.Inventory(c.user.ID)
	if err != nil {
		return err
	}
	bySlot := map[int]int{}
	for index, item := range items {
		bySlot[item.Slot] = index
	}
	targetIndex, ok := bySlot[targetSlot]
	if !ok {
		s.enhanceError(c)
		return nil
	}
	target := items[targetIndex]
	limit, ok := s.enhanceMax[target.ID]
	if !ok || target.Level >= 9 || target.Level >= limit.TotalMaxLv {
		s.enhanceError(c)
		return nil
	}
	grade := s.items[target.ID].Grade
	row, ok := s.enhanceExp[target.Level]
	if !ok || grade < 0 || grade >= len(row) || row[grade] <= 0 {
		s.enhanceError(c)
		return nil
	}
	attrs := unpackEnhanceAttributes(target.Value)
	currentLevel := enhanceLevel(attrs)
	if currentLevel >= limit.TotalMaxLv {
		s.enhanceError(c)
		return nil
	}
	materialSlots := make([]int, 0, len(materialGameSlots))
	seen := map[int]bool{targetSlot: true}
	totalExp := 0
	for _, gameSlot := range materialGameSlots {
		slot := gameSlot - defaults
		if slot <= 0 || seen[slot] {
			s.enhanceError(c)
			return nil
		}
		index, ok := bySlot[slot]
		if !ok {
			s.enhanceError(c)
			return nil
		}
		material := items[index]
		seen[slot] = true
		materialSlots = append(materialSlots, slot)
		materialGrade := s.items[material.ID].Grade
		if materialGrade == 0 {
			switch material.ID {
			case 8481:
				materialGrade = 4
			case 8482:
				materialGrade = 5
			case 8566:
				materialGrade = 3
			}
		}
		if materialGrade < 0 || materialGrade >= len(enhanceItemExp) {
			continue
		}
		exp := enhanceItemExp[materialGrade]
		if material.Level > 0 {
			exp = int(float64(exp) * float64(material.Level) * 1.4)
		}
		if material.ID == target.ID {
			exp *= 2
		}
		totalExp += exp
	}
	newExp := target.Exp + totalExp
	if newExp > row[grade] {
		newExp = row[grade]
	}
	chance := newExp * 100 / row[grade]
	if chance > 100 {
		chance = 100
	}
	status := 1
	attribute := -1
	newLevel := target.Level
	newValue := target.Value
	if rand.IntN(100) < chance {
		index, ok := s.enhanceAttribute(attrs, currentLevel, limit.Caps)
		if !ok {
			s.enhanceError(c)
			return nil
		}
		attrs[index]++
		status = 0
		attribute = index
		newLevel++
		newValue = packEnhanceAttributes(attrs)
	}
	changed, err := s.Store.EnhanceItem(c.user.ID, targetSlot, materialSlots, newLevel, 0, newValue)
	if err != nil {
		return err
	}
	if err = s.sendInventoryItems(c, changed); err != nil {
		return err
	}
	s.enhanceReply(c, status, targetGameSlot, enhanceLevel(attrs), attribute)
	return nil
}
