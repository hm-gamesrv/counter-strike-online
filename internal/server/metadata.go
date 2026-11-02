package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

type metadataEntry struct {
	Name, File string
	ID         int
	Raw        bool
}

const (
	metadataFullZip   = 5
	metadataChunkSize = 64000
	metadataMaxChunks = 4
)

// zipMetadataFrames wraps a zip stream the way upstream does: a single
// U8(id)/U8(FULL_ZIP)/U16(size) frame, or up to four 64000-byte chunks where
// the last one is marked with metadataMaxChunks.
func zipMetadataFrames(id int, data []byte) [][]byte {
	if len(data) > metadataChunkSize {
		frames := make([][]byte, 0, metadataMaxChunks)
		for i := 0; i < metadataMaxChunks && len(data) > 0; i++ {
			n := len(data)
			if n > metadataChunkSize {
				n = metadataChunkSize
			}
			w := &protocol.Writer{}
			w.U8(id)
			if i == metadataMaxChunks-1 || n == len(data) {
				w.U8(metadataMaxChunks)
			} else {
				w.U8(i + 1)
			}
			w.U16(n)
			w.Bytes(data[:n])
			frames = append(frames, w.Data)
			data = data[n:]
		}
		return frames
	}
	w := &protocol.Writer{}
	w.U8(id)
	w.U8(metadataFullZip)
	w.U16(len(data))
	w.Bytes(data)
	return [][]byte{w.Data}
}

var metadataFiles = []metadataEntry{
	{"ModeList", "Metadata_ModeList.bin", 2, true},
	{"Encyclopedia", "Metadata_Encyclopedia.bin", 23, true},
	{"Maplist", "MapList.csv", 0, false},
	{"ClientTable", "ClientTable.csv", 1, false},
	{"Unk3", "Metadata_Unk3.bin", 3, true},
	{"Unk8", "Metadata_Unk8.bin", 8, true},
	{"MatchOption", "MatchOption.csv", 9, false},
	{"WeaponParts", "weaponparts.csv", 17, false},
	{"MileageShop", "MileageShop.csv", 18, false},
	{"Unk20", "Metadata_Unk20.bin", 20, true},
	{"GameModeList", "GameModeList.csv", 24, false},
	{"ProgressUnlock", "progress_unlock.csv", 27, false},
	{"ReinforceMaxLvl", "ReinforceMaxLv.csv", 28, false},
	{"ReinforceMaxEXP", "ReinforceMaxEXP.csv", 29, false},
	{"ReinforceItemsExp", "Metadata_ReinforceItemsExp.bin", 30, true},
	{"Unk31", "Metadata_Unk31.bin", 31, true},
	{"Item", "Item.csv", 32, false},
	{"CodisData", "CodisData.csv", 35, false},
	{"HonorMoneyShop", "HonorMoneyShop.csv", 36, false},
	{"ItemExpireTime", "ItemExpireTime.csv", 37, false},
	{"ScenarioTX_Common", "scenariotx_common.json", 38, false},
	{"ScenarioTX_Dedi", "scenariotx_dedi.json", 39, false},
	{"ShopItemList_Dedi", "shopitemlist_dedi.json", 40, false},
	{"WeaponProp", "WeaponProp.json", 42, false},
	{"Unk43", "Metadata_Unk43.bin", 43, true},
	{"PPSystem", "ppsystem.json", 45, false},
	{"ZBCompetitive", "ZBCompetitive.json", 48, false},
	{"Unk49", "Metadata_Unk49.bin", 49, true},
	{"ModeEvent", "ModeEvent.csv", 50, false},
	{"EventShop", "EventShop.csv", 51, false},
	{"FamilyTotalWarMap", "FamilyTotalWarMap.csv", 52, false},
	{"FamilyTotalWar", "FamilyTotalWar.json", 53, false},
	{"Unk54", "Metadata_Unk54.bin", 54, true},
	{"Unk55", "Metadata_Unk55.bin", 55, true},
}

func loadMetadata(dir string, c config.Config) ([][]byte, error) {
	var packets [][]byte
	for _, entry := range metadataFiles {
		if !c.Metadata[entry.Name] {
			continue
		}
		b, e := os.ReadFile(filepath.Join(dir, "Data", entry.File))
		if e != nil {
			return nil, e
		}
		if !entry.Raw {
			var buf bytes.Buffer
			z := zip.NewWriter(&buf)
			f, e := z.Create(entry.File)
			if e != nil {
				return nil, e
			}
			if _, e = f.Write(b); e != nil {
				return nil, e
			}
			if e = z.Close(); e != nil {
				return nil, e
			}
			for _, frame := range zipMetadataFrames(entry.ID, buf.Bytes()) {
				if len(frame)+5 > protocol.MaxFrame {
					return nil, fmt.Errorf("metadata %s exceeds frame limit", entry.File)
				}
				packets = append(packets, frame)
			}
			continue
		}
		if len(b)+5 > protocol.MaxFrame {
			return nil, fmt.Errorf("metadata %s exceeds frame limit", entry.File)
		}
		packets = append(packets, b)
	}
	if c.Metadata["WeaponPaints"] {
		var paints map[string]json.RawMessage
		if e := config.ReadJSON(filepath.Join(dir, "WeaponPaints.json"), &paints); e != nil {
			return nil, e
		}
		w := protocol.Writer{}
		w.U8(6)
		keys := numericKeys(paints)
		w.U16(len(keys))
		for _, k := range keys {
			var p struct{ Paints []int }
			if e := json.Unmarshal(paints[k], &p); e != nil {
				return nil, e
			}
			id, _ := strconv.Atoi(k)
			w.U16(id)
			w.U16(len(p.Paints))
			for _, v := range p.Paints {
				w.U16(v)
			}
		}
		packets = append(packets, w.Data)
	}
	if c.Metadata["ZombieWarWeaponList"] {
		var p struct{ Weapons []int }
		if e := config.ReadJSON(filepath.Join(dir, "ZombieWarWeaponList.json"), &p); e != nil {
			return nil, e
		}
		w := protocol.Writer{}
		w.U8(15)
		w.U16(len(p.Weapons))
		for _, v := range p.Weapons {
			w.U32(v)
		}
		packets = append(packets, w.Data)
	}
	if c.Metadata["RandomWeaponList"] {
		var p map[string]json.RawMessage
		if e := config.ReadJSON(filepath.Join(dir, "RandomWeaponList.json"), &p); e != nil {
			return nil, e
		}
		keys := numericKeys(p)
		w := protocol.Writer{}
		w.U8(16)
		w.U32(len(keys))
		for _, k := range keys {
			id, _ := strconv.Atoi(k)
			w.U32(id)
			var modes map[string]struct{ DropRate, EnhanceProbability int }
			if e := json.Unmarshal(p[k], &modes); e != nil {
				return nil, e
			}
			mk := numericKeys(modes)
			w.U32(len(mk))
			for _, m := range mk {
				id, _ := strconv.Atoi(m)
				w.U8(id)
				w.U32(modes[m].DropRate)
				w.U32(modes[m].EnhanceProbability)
			}
		}
		packets = append(packets, w.Data)
	}
	if c.Metadata["ItemBox"] {
		var boxes map[string]struct {
			Rate map[string]struct{ Items []int }
		}
		var raw map[string]json.RawMessage
		if e := config.ReadJSON(filepath.Join(dir, "ItemBox.json"), &raw); e != nil {
			return nil, e
		}
		delete(raw, "Version")
		b, e := json.Marshal(raw)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &boxes); e != nil {
			return nil, e
		}
		w := &protocol.Writer{}
		w.U8(4)
		count := 0
		for _, box := range boxes {
			for _, rate := range box.Rate {
				count += len(rate.Items)
			}
		}
		w.U32(count)
		for _, k := range numericKeys(boxes) {
			id, _ := strconv.Atoi(k)
			rates := boxes[k].Rate
			keys := make([]string, 0, len(rates))
			for key := range rates {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				for _, item := range rates[key].Items {
					w.U32(id)
					w.U32(item)
					w.U16(65535)
					w.U16(1)
					w.Bytes([]byte{3, 0, 1, 0})
					w.U32(0)
					w.U16(0)
					w.U8(0)
					w.U16(0)
					w.U8(0)
				}
			}
		}
		packets = append(packets, w.Data)
	}
	for _, b := range packets {
		if len(b)+5 > protocol.MaxFrame {
			return nil, fmt.Errorf("metadata payload exceeds frame limit: %d", len(b))
		}
	}
	return packets, nil
}
func numericKeys[T any](m map[string]T) []string {
	var keys []string
	for k := range m {
		if _, e := strconv.Atoi(k); e == nil {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { a, _ := strconv.Atoi(keys[i]); b, _ := strconv.Atoi(keys[j]); return a < b })
	return keys
}
