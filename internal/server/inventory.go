package server

import (
	"encoding/binary"
	"errors"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func defaultInventoryPacket(ids []int) (*protocol.Writer, error) {
	w := &protocol.Writer{}
	w.U16(len(ids))
	for slot, id := range ids {
		if id < 1 || id > 65535 || slot > 65535 {
			return nil, errors.New("default inventory item exceeds protocol range")
		}
		writeItem(w, store.Item{ID: id, Count: 1, Status: 1, InUse: 1, Lock: 2}, slot, 3)
		if len(w.Data) > protocol.MaxPayload {
			return nil, errors.New("default inventory exceeds frame limit")
		}
	}
	return w, nil
}

// Item sizes vary with paints and parts. Split on encoded bytes, including the
// inventory header, and validate every entry before sending any packet.
func inventoryPackets(items []store.Item, defaultSlots int) ([][]byte, error) {
	if defaultSlots < 0 || defaultSlots >= 65535 {
		return nil, errors.New("invalid default inventory slot count")
	}
	var packets [][]byte
	w := &protocol.Writer{Data: make([]byte, 6)}
	count := 0
	flush := func() {
		binary.LittleEndian.PutUint16(w.Data[4:6], uint16(count))
		packets = append(packets, w.Data)
		w = &protocol.Writer{Data: make([]byte, 6)}
		count = 0
	}
	for _, item := range items {
		if item.Slot < 1 || item.Slot > 65535-defaultSlots || len(item.Paints) > 65535 {
			return nil, errors.New("inventory item exceeds protocol range")
		}
		entry := &protocol.Writer{}
		writeItem(entry, item, item.Slot+defaultSlots, 0)
		if len(entry.Data) > protocol.MaxPayload-6 {
			return nil, errors.New("inventory item exceeds frame limit")
		}
		if len(w.Data)+len(entry.Data) > protocol.MaxPayload {
			flush()
		}
		w.Bytes(entry.Data)
		count++
	}
	flush() // An empty full inventory still needs a zero-count response.
	return packets, nil
}

func (s *Server) sendInventoryItems(c *client, items []store.Item) error {
	packets, err := inventoryPackets(items, len(s.Config.DefaultUser.DefaultItems))
	if err != nil {
		return err
	}
	for _, data := range packets {
		c.send(154, &protocol.Writer{Data: data})
	}
	return nil
}

func (s *Server) itemRequest(c *client, p *protocol.Reader) error {
	kind := p.U8()
	var slot, inventoryType, count int
	var action store.ItemAction
	switch kind {
	case 0:
		inventoryType = p.U8()
		slot = p.U16()
		p.U8()
		count = p.U16()
		action = store.SelectItem
	case 23, 26:
		p.U8() // Upstream treats these as activate/toggle, not arbitrary setters.
		slot = p.U16()
		action = store.ActivateItem
		if kind == 26 {
			action = store.ToggleItemLock
		}
	case 10, 11, 12: // Background decoder/gift notifications do not open dialogs.
		return p.Err
	default:
		s.Log.Debug("unsupported item request", "kind", kind, "user", c.user.ID)
		return p.Err
	}
	if p.Err != nil {
		return p.Err
	}
	if p.Pos != len(p.Data) {
		return errors.New("unexpected item request tail")
	}
	slot -= len(s.Config.DefaultUser.DefaultItems)
	items, err := s.Store.Inventory(c.user.ID)
	if err != nil {
		return err
	}
	var target store.Item
	for _, item := range items {
		if item.Slot == slot {
			target = item
			break
		}
	}
	if target.ID == 0 || slot <= 0 {
		c.notice("Item unavailable.")
		return nil
	}
	if kind == 0 && inventoryType != 1 && s.items[target.ID].RewardID != 0 {
		return s.useRewardItem(c, target, count)
	}
	if action != store.ToggleItemLock {
		definition, ok := s.items[target.ID]
		weapon := definition.Category >= 1 && definition.Category <= 6 || definition.Category == 11
		if !ok || (!weapon && definition.Category != 7) || kind == 0 && inventoryType == 1 && !weapon {
			c.notice("This item cannot be activated here.")
			return nil
		}
	}
	protected := append([]int{}, s.Config.DefaultUser.DefaultItems...)
	protected = append(protected, s.Config.DefaultUser.PseudoDefaultItems...)
	changed, err := s.Store.ChangeItem(c.user.ID, slot, action, protected)
	if errors.Is(err, store.ErrItemUnavailable) || errors.Is(err, store.ErrItemProtected) {
		c.notice(err.Error())
		return nil
	}
	if err != nil {
		return err
	}
	return s.sendInventoryItems(c, changed)
}
