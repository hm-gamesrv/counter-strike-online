package server

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strconv"

	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

type randomItemBin struct {
	Chance int
	Items  []store.Reward
}
type randomDurationBin struct {
	Chance    int
	Durations []int
}
type randomRewardGroup struct {
	Items     []randomItemBin
	Durations []randomDurationBin
}

func compileRandomRewards(raw json.RawMessage) ([]randomRewardGroup, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var groups []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	var result []randomRewardGroup
	for _, group := range groups {
		var out randomRewardGroup
		keys := make([]string, 0, len(group))
		for key := range group {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "Durations" {
				var durations map[string][]int
				if err := json.Unmarshal(group[key], &durations); err != nil {
					return nil, err
				}
				dkeys := make([]string, 0, len(durations))
				for k := range durations {
					dkeys = append(dkeys, k)
				}
				sort.Strings(dkeys)
				for _, k := range dkeys {
					chance, err := strconv.Atoi(k)
					if err != nil || chance < 0 || chance > 100 || len(durations[k]) == 0 {
						return nil, errors.New("invalid random reward duration bin")
					}
					for _, v := range durations[k] {
						if v < -1 || v > 36500 {
							return nil, errors.New("invalid random reward duration")
						}
					}
					out.Durations = append(out.Durations, randomDurationBin{chance, durations[k]})
				}
				continue
			}
			chance, err := strconv.Atoi(key)
			if err != nil || chance < 0 || chance > 100 {
				return nil, errors.New("invalid random reward chance")
			}
			var items map[string]json.RawMessage
			if err = json.Unmarshal(group[key], &items); err != nil {
				return nil, err
			}
			bin := randomItemBin{Chance: chance}
			for _, idText := range numericKeys(items) {
				id, _ := strconv.Atoi(idText)
				v := struct{ Count, Duration int }{Count: 1}
				if err = json.Unmarshal(items[idText], &v); err != nil {
					return nil, err
				}
				if id < 1 || id > 65535 || v.Count < 1 || v.Count > 65535 || v.Duration < -1 || v.Duration > 36500 {
					return nil, errors.New("invalid random reward item")
				}
				bin.Items = append(bin.Items, store.Reward{ID: id, Count: v.Count, Duration: v.Duration})
			}
			if len(bin.Items) == 0 || len(bin.Items) != len(items) {
				return nil, errors.New("empty or invalid random item bin")
			}
			out.Items = append(out.Items, bin)
		}
		if len(out.Items) == 0 {
			return nil, errors.New("random reward group has no items")
		}
		result = append(result, out)
	}
	return result, nil
}

func randomRewardIndex(n int) (int, error) {
	if n < 1 {
		return 0, errors.New("empty random reward choice")
	}
	i, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(i.Int64()), nil
}

// Match the C++ ordered chance tests and unconditional last-bin fallback.
// These are conditional percentage bins, not normalized weights.
func drawRandomReward(group randomRewardGroup) (store.Reward, error) {
	var item store.Reward
	if len(group.Items) == 0 {
		return item, errors.New("empty random reward group")
	}
	for i, bin := range group.Items {
		roll, err := randomRewardIndex(100)
		if err != nil {
			return item, err
		}
		if roll < bin.Chance || i == len(group.Items)-1 {
			index, err := randomRewardIndex(len(bin.Items))
			if err != nil {
				return item, err
			}
			item = bin.Items[index]
			break
		}
	}
	if item.Duration != -1 {
		for i, bin := range group.Durations {
			roll, err := randomRewardIndex(100)
			if err != nil {
				return item, err
			}
			if roll < bin.Chance || i == len(group.Durations)-1 {
				duration, err := randomRewardValue(bin.Durations)
				if err != nil {
					return item, err
				}
				item.Duration = duration
				break
			}
		}
	}
	return item, nil
}
