package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

type product struct {
	Item   int
	Points bool
	Subs   []subproduct
}
type subproduct struct {
	ID, Price, AdditionalPoints, AdType int
	Items                               []store.Reward
}
type shop struct {
	Products    []product
	Recommended map[string][]int
	Popular     []int
}

// JSON insertion order determines upstream product IDs; sorting the keys would
// silently sell a different product for the same ID after migration.
func orderedKeys(raw json.RawMessage) ([]string, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil {
		return nil, e
	}
	if t != json.Delim('{') {
		return nil, errors.New("expected JSON object")
	}
	var keys []string
	seen := map[string]bool{}
	for d.More() {
		t, e = d.Token()
		if e != nil {
			return nil, e
		}
		key, ok := t.(string)
		if !ok {
			return nil, errors.New("invalid object key")
		}
		if seen[key] {
			return nil, errors.New("duplicate shop object key: " + key)
		}
		seen[key] = true
		keys = append(keys, key)
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			return nil, e
		}
	}
	return keys, nil
}
func loadShop(dir string) (shop, error) {
	var src struct {
		Version     int
		Products    json.RawMessage
		Recommended map[string][]int
		Popular     []int
	}
	var result shop
	if e := config.ReadJSON(filepath.Join(dir, "Shop.json"), &src); e != nil {
		return result, e
	}
	if src.Version != 1 {
		return result, errors.New("unsupported Shop.json version")
	}
	result.Recommended = src.Recommended
	result.Popular = src.Popular
	keys, e := orderedKeys(src.Products)
	if e != nil {
		return result, e
	}
	var products map[string]struct {
		IsPoints    bool
		SubProducts json.RawMessage
	}
	if e = json.Unmarshal(src.Products, &products); e != nil {
		return result, e
	}
	id := 1
	for _, key := range keys {
		item, e := strconv.Atoi(key)
		if e != nil {
			return result, e
		}
		if item < 1 || item > 65535 {
			return result, errors.New("shop item ID outside protocol range")
		}
		p := product{Item: item, Points: products[key].IsPoints}
		subs := products[key].SubProducts
		sk, e := orderedKeys(subs)
		if e != nil {
			return result, e
		}
		if len(sk) < 1 || len(sk) > 255 {
			return result, errors.New("shop subproduct count must be 1..255")
		}
		var values map[string]struct {
			Price, AdditionalPoints, AdType, Count, Duration int
			Items                                            json.RawMessage
		}
		if e = json.Unmarshal(subs, &values); e != nil {
			return result, e
		}
		for _, key := range sk {
			v := values[key]
			sp := subproduct{ID: id, Price: v.Price, AdditionalPoints: v.AdditionalPoints, AdType: v.AdType}
			id++
			if v.Price < 0 || v.AdditionalPoints < 0 || uint64(v.Price) > 1<<32-1 || uint64(v.AdditionalPoints) > 1<<32-1 || v.AdType < 0 || v.AdType > 255 {
				return result, errors.New("product price, bonus or advertisement outside protocol range")
			}
			if len(v.Items) == 0 {
				count := v.Count
				if count == 0 {
					count = 1
				}
				sp.Items = []store.Reward{{ID: item, Count: count, Duration: v.Duration}}
			} else {
				ik, e := orderedKeys(v.Items)
				if e != nil {
					return result, e
				}
				var iv map[string]struct{ Count, Duration int }
				if e = json.Unmarshal(v.Items, &iv); e != nil {
					return result, e
				}
				for _, key := range ik {
					item, e := strconv.Atoi(key)
					if e != nil {
						return result, e
					}
					count := iv[key].Count
					if count == 0 {
						count = 1
					}
					sp.Items = append(sp.Items, store.Reward{ID: item, Count: count, Duration: iv[key].Duration})
				}
			}
			if len(sp.Items) == 0 {
				return result, errors.New("empty shop product")
			}
			for _, reward := range sp.Items {
				if reward.ID < 1 || reward.ID > 65535 || reward.Count < 1 || reward.Count > 65535 || reward.Duration < -1 || reward.Duration > 36500 {
					return result, errors.New("shop reward outside protocol range")
				}
			}
			p.Subs = append(p.Subs, sp)
		}
		result.Products = append(result.Products, p)
	}
	if len(result.Products) > 255 {
		return result, errors.New("too many shop products for protocol")
	}
	for _, packet := range result.packets() {
		if len(packet.Data) > protocol.MaxPayload {
			return result, errors.New("shop data exceeds frame limit")
		}
	}
	return result, nil
}
func (s shop) packets() []*protocol.Writer {
	var packets []*protocol.Writer
	w := &protocol.Writer{}
	w.U8(0)
	w.U8(len(s.Products))
	for _, p := range s.Products {
		w.U32(p.Item)
		if p.Points {
			w.U8(1)
		} else {
			w.U8(0)
		}
		w.U8(len(p.Subs))
		for _, sp := range p.Subs {
			w.U32(sp.ID)
			w.U16(sp.Items[0].Duration)
			w.U8(1)
			w.U16(sp.Items[0].Count)
			w.U32(sp.Price)
			w.U32(sp.AdditionalPoints)
			w.U8(sp.AdType)
			w.U8(0)
		}
	}
	w.String("UAH")
	packets = append(packets, w)
	w = &protocol.Writer{}
	w.U8(15)
	keys := numericKeys(s.Recommended)
	w.U32(len(keys))
	for _, k := range keys {
		// Approved deviation: the reference hard-codes the placeholder strings
		// "Test"/"Test2" here, which the client renders as the page title. The
		// page keeps a real caption instead; the rest of the block is identical.
		w.String("Recommended")
		w.String("")
		w.U32(0)
		w.U32(len(s.Recommended[k]))
		for _, id := range s.Recommended[k] {
			w.U32(id)
		}
	}
	packets = append(packets, w)
	w = &protocol.Writer{}
	w.U8(16)
	w.U32(len(s.Popular))
	for _, id := range s.Popular {
		w.U32(id)
	}
	return append(packets, w)
}
func (s *Server) sendShop(c *client) {
	for _, packet := range s.shop.packets() {
		c.send(72, packet)
	}
}
func (s *Server) shopRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if r.Err != nil {
		return r.Err
	}
	if kind == 3 {
		s.sendShop(c)
		return r.Err
	}
	if kind != 1 {
		return nil
	}
	// Current 30.12.24 client: product ID as uint32 LE.
	if len(r.Data) != 5 {
		return errors.New("invalid shop purchase length")
	}
	id := r.U32()
	if r.Err != nil {
		return r.Err
	}
	reply := "BUY_FAIL_NOITEM"
	for _, p := range s.shop.Products {
		for _, sp := range p.Subs {
			if sp.ID != id {
				continue
			}
			items, err := s.prepareGrants(sp.Items)
			if err != nil {
				reply = "BUY_FAIL_SYSTEM_ERROR"
				continue
			}
			var touched []store.Item
			touched, err = s.Store.PurchaseTracked(c.user.ID, p.Points, sp.Price, sp.AdditionalPoints, items, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
			if err != nil {
				reply = err.Error()
			} else {
				reply = "BUY_OK"
				s.autoEquipCostumes(c.user.ID, items)
				var e error
				c.character, e = s.Store.Character(c.user.ID)
				if e != nil {
					return e
				}
				// The reference sends the rows AddItem added or incremented, not a
				// full bag; the 154 frame is a slot-keyed patch.
				if e = s.sendInventoryItems(c, touched); e != nil {
					return e
				}
				w := &protocol.Writer{}
				w.U32(c.user.ID)
				userInfo(w, c.character)
				c.send(157, w)
			}
		}
	}
	w := &protocol.Writer{}
	w.U8(1)
	// SendShopBuyProductReply writes this constant unconditionally; the result
	// travels in the reply string.
	w.U8(1)
	w.String(reply)
	w.U16(0)
	c.send(72, w)
	return nil
}
