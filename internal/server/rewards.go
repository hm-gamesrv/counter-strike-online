package server

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"path/filepath"
	"strconv"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

type itemReward struct {
	Title, Description       string
	Localized, Select        bool
	LvlRestriction           int
	Points, Exp, HonorPoints []int
	Items                    map[string]struct{ Count, Duration int }
	RandomItems              json.RawMessage
	randomGroups             []randomRewardGroup
}

func loadRewards(dir string) (map[int]itemReward, error) {
	var raw map[string]json.RawMessage
	if err := config.ReadJSON(filepath.Join(dir, "ItemRewards.json"), &raw); err != nil {
		return nil, err
	}
	var version int
	if err := json.Unmarshal(raw["Version"], &version); err != nil || version != 1 {
		return nil, errors.New("unsupported ItemRewards.json version")
	}
	result := map[int]itemReward{}
	for key, value := range raw {
		if key == "Version" {
			continue
		}
		id, err := strconv.Atoi(key)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("invalid reward ID %q", key)
		}
		var reward itemReward
		if err = json.Unmarshal(value, &reward); err != nil {
			return nil, err
		}
		if reward.LvlRestriction < 0 {
			return nil, errors.New("negative reward level restriction")
		}
		for _, values := range [][]int{reward.Points, reward.Exp, reward.HonorPoints} {
			for _, v := range values {
				if v < 0 || uint64(v) > math.MaxUint32 {
					return nil, errors.New("reward currency outside protocol range")
				}
			}
		}
		for k, item := range reward.Items {
			itemID, err := strconv.Atoi(k)
			if err != nil || itemID < 1 || itemID > 65535 || item.Count < 1 || item.Count > 65535 || item.Duration < -1 || item.Duration > 36500 {
				return nil, fmt.Errorf("invalid item in reward %d", id)
			}
		}
		reward.randomGroups, err = compileRandomRewards(reward.RandomItems)
		if err != nil {
			return nil, fmt.Errorf("reward %d: %w", id, err)
		}
		if reward.Select && (len(reward.Items) == 0 || len(reward.Items) > 255) {
			return nil, errors.New("choice reward must have 1..255 options")
		}
		result[id] = reward
	}
	return result, nil
}

// Apply the selected client's catalog semantics once, for every award source.
func (s *Server) prepareGrants(items []store.Reward) ([]store.Reward, error) {
	result := append([]store.Reward{}, items...)
	for i := range result {
		item := &result[i]
		definition, ok := s.items[item.ID]
		if !ok {
			return nil, fmt.Errorf("unknown reward item %d", item.ID)
		}
		if item.Duration == -1 {
			item.Duration = 0
			item.Dormant = true
		}
		item.Exclusive = definition.Category >= 1 && definition.Category <= 6 || definition.Category == 11
		item.Unselected = definition.Category == 9 && definition.UseType >= 0 && definition.UseType <= 2 || definition.Category == 8 && (definition.UseType == 0 || definition.UseType == 2)
		item.Stackable = item.Unselected
		switch definition.ClassName {
		case "Tattoo", "LobbyBG", "zbRespawnEffect", "CombatInfoItem":
			item.Dormant = true
		}
		item.Dormant = item.Dormant || definition.Category == 12
	}
	return result, nil
}

func randomRewardValue(values []int) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}
	if len(values) == 1 {
		return values[0], nil
	}
	i, err := rand.Int(rand.Reader, big.NewInt(int64(len(values))))
	if err != nil {
		return 0, err
	}
	return values[i.Int64()], nil
}

func rewardNotice(reward itemReward, items []store.Reward, g store.Grant) *protocol.Writer {
	w := &protocol.Writer{}
	if reward.Localized {
		w.U8(60)
	} else {
		w.U8(61)
	}
	w.U16(len(items))
	for _, item := range items {
		w.U16(item.ID)
		w.U16(item.Count)
		w.U16(item.Duration)
		w.U16(0)
		w.U32(0)
		w.Bytes([]byte{0, 0, 0})
	}
	w.Bytes([]byte{0, 0})
	for _, v := range []int{g.Points, g.Exp, g.HonorPoints} {
		if v == 0 {
			w.U8(0)
		} else {
			w.U8(1)
			w.U32(v)
		}
	}
	w.Bytes([]byte{0, 0, 0, 0, 0})
	w.String(reward.Title)
	w.String(reward.Description)
	w.U8(0)
	return w
}

// refreshItems re-reads the bag after an out-of-band rewrite and replaces the
// snapshot entries that still exist. Entries whose slot is gone are consumed
// sources and stay as tombstones for the client.
func (s *Server) refreshItems(id int, snapshot []store.Item) ([]store.Item, error) {
	fresh, err := s.Store.Inventory(id)
	if err != nil {
		return nil, err
	}
	bySlot := make(map[int]store.Item, len(fresh))
	for _, item := range fresh {
		bySlot[item.Slot] = item
	}
	refreshed := make([]store.Item, 0, len(snapshot))
	for _, item := range snapshot {
		if current, ok := bySlot[item.Slot]; ok {
			refreshed = append(refreshed, current)
			continue
		}
		refreshed = append(refreshed, item)
	}
	return refreshed, nil
}

// autoEquipCostumes applies the upstream AddItem rule: a newly granted costume
// fills an empty loadout slot automatically. Returns the item IDs equipped.
func (s *Server) autoEquipCostumes(id int, items []store.Reward) map[int]bool {
	equipped := map[int]bool{}
	for _, item := range items {
		definition, ok := s.items[item.ID]
		if !ok || !store.IsCostumeClass(definition.ClassName) {
			continue
		}
		changed, err := s.Store.AutoEquipCostume(id, item.ID, definition.ClassName, definition.ZombieSkin)
		if err != nil {
			if s.Log != nil {
				s.Log.Warn("costume auto-equip failed", "user", id, "item", item.ID, "error", err)
			}
			continue
		}
		if changed {
			equipped[item.ID] = true
		}
	}
	return equipped
}
