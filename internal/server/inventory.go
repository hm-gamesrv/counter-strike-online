package server

import (
	"encoding/binary"
	"errors"
	"strings"

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

const maxOpenItemBoxes = 100

func (s *Server) itemBoxError(c *client, text string) {
	w := &protocol.Writer{}
	w.U8(4)
	w.U8(0)
	w.String(text)
	c.send(78, w)
}

// openItemBox implements the decoder path (Item subtype 4). The upstream call
// passes both uint16 values through an unspecified argument-evaluation order,
// so accept either order and anchor it on an owned LotteryKeyItem. When both
// readings match, the larger value is the slot, which is the normal inventory
// layout and also handles the count-first client build.
func (s *Server) openItemBox(c *client, first, second int) error {
	if c.playing {
		s.itemBoxError(c, "LOTTERY_FAIL_NOITEM")
		return nil
	}
	items, err := s.Store.Inventory(c.user.ID)
	if err != nil {
		return err
	}
	defaultSlots := len(s.Config.DefaultUser.DefaultItems)
	type candidate struct {
		slot  int
		count int
		item  store.Item
	}
	var candidates []candidate
	for _, reading := range []struct{ slot, count int }{
		{first - defaultSlots, second},
		{second - defaultSlots, first},
	} {
		if reading.slot <= 0 || reading.count < 1 || reading.count > maxOpenItemBoxes {
			continue
		}
		for _, item := range items {
			if item.Slot == reading.slot && item.ID != 0 && s.items[item.ID].ClassName == "LotteryKeyItem" && item.Count >= reading.count {
				candidates = append(candidates, candidate{slot: reading.slot, count: reading.count, item: item})
				break
			}
		}
	}
	if len(candidates) == 0 {
		s.itemBoxError(c, "LOTTERY_FAIL_NOITEM")
		return nil
	}
	chosen := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.slot > chosen.slot {
			chosen = candidate
		}
	}
	target, slot, count := chosen.item, chosen.slot, chosen.count
	box := s.itemBoxes[target.ID]
	if box == nil {
		s.itemBoxError(c, "LOTTERY_FAIL_NOITEM")
		return nil
	}
	awards := make([]store.Reward, 0, count)
	results := make([]itemBoxAward, 0, count)
	for i := 0; i < count; i++ {
		award, err := box.roll()
		if err != nil {
			s.Log.Warn("item box roll failed", "user", c.user.ID, "box", target.ID, "error", err)
			s.itemBoxError(c, "LOTTERY_FAIL_NOITEM")
			return nil
		}
		results = append(results, award)
		awards = append(awards, store.Reward{ID: award.ID, Count: 1, Duration: max(award.Duration, 0)})
	}
	changed, err := s.Store.OpenItemBox(c.user.ID, slot, target.ID, count, awards, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
	if err != nil {
		switch {
		case errors.Is(err, store.ErrItemUnavailable):
			s.itemBoxError(c, "LOTTERY_FAIL_NOITEM")
		case strings.Contains(err.Error(), "INVENTORY_FULL"):
			s.itemBoxError(c, "LOTTERY_FAIL_INVENTORY_FULL")
		default:
			s.Log.Warn("item box open failed", "user", c.user.ID, "box", target.ID, "error", err)
			s.itemBoxError(c, "LOTTERY_FAIL_NOITEM")
		}
		return nil
	}
	if err = s.sendInventoryItems(c, changed); err != nil {
		return err
	}
	w := &protocol.Writer{}
	w.U8(4)
	w.U8(1)
	w.U32(target.ID)
	w.U16(0)
	w.U32(0xFAD)
	w.U8(len(results))
	for _, award := range results {
		w.U32(award.ID)
		w.U16(award.Duration)
		w.U16(1)
		w.U8(award.Grade)
		w.U8(0)
		w.U16(0)
		w.U16(0)
	}
	c.send(78, w)
	return nil
}

func (s *Server) extendItem(c *client, gameSlot, days int) error {
	slot := gameSlot - len(s.Config.DefaultUser.DefaultItems)
	if slot <= 0 {
		c.notice("Item unavailable.")
		return nil
	}
	changed, err := s.Store.ExtendItem(c.user.ID, slot, 676, days)
	if errors.Is(err, store.ErrItemUnavailable) {
		c.notice("Item unavailable.")
		return nil
	}
	if err != nil {
		return err
	}
	return s.sendInventoryItems(c, changed)
}

func containsPaint(paints []int, paint int) bool {
	for _, candidate := range paints {
		if candidate == paint {
			return true
		}
	}
	return false
}

func (s *Server) paintReply(c *client) {
	c.send(78, &protocol.Writer{Data: []byte{17, 1, 1, 1}})
}

func (s *Server) applyWeaponPaint(c *client, weaponGameSlot, paintGameSlot int) error {
	defaults := len(s.Config.DefaultUser.DefaultItems)
	weaponSlot, paintSlot := weaponGameSlot-defaults, paintGameSlot-defaults
	if weaponSlot <= 0 || paintSlot <= 0 {
		c.notice("Item unavailable.")
		return nil
	}
	items, err := s.Store.Inventory(c.user.ID)
	if err != nil {
		return err
	}
	var weapon, paint store.Item
	for _, item := range items {
		if item.Slot == weaponSlot {
			weapon = item
		}
		if item.Slot == paintSlot {
			paint = item
		}
	}
	if weapon.ID == 0 || paint.ID == 0 || !containsPaint(s.weaponPaints[weapon.ID], paint.ID) {
		c.notice("Item unavailable.")
		return nil
	}
	changed, err := s.Store.ApplyWeaponPaint(c.user.ID, weaponSlot, paintSlot, paint.ID)
	if errors.Is(err, store.ErrItemUnavailable) {
		c.notice("Item unavailable.")
		return nil
	}
	if err != nil {
		return err
	}
	if err = s.sendInventoryItems(c, changed); err != nil {
		return err
	}
	s.paintReply(c)
	return nil
}

func (s *Server) switchWeaponPaint(c *client, weaponGameSlot, paintID int) error {
	slot := weaponGameSlot - len(s.Config.DefaultUser.DefaultItems)
	if slot <= 0 {
		c.notice("Item unavailable.")
		return nil
	}
	items, err := s.Store.Inventory(c.user.ID)
	if err != nil {
		return err
	}
	var weapon store.Item
	for _, item := range items {
		if item.Slot == slot {
			weapon = item
			break
		}
	}
	if weapon.ID == 0 || paintID != 0 && !containsPaint(s.weaponPaints[weapon.ID], paintID) {
		c.notice("Item unavailable.")
		return nil
	}
	changed, err := s.Store.SwitchWeaponPaint(c.user.ID, slot, paintID)
	if errors.Is(err, store.ErrItemUnavailable) {
		c.notice("Item unavailable.")
		return nil
	}
	if err != nil {
		return err
	}
	if err = s.sendInventoryItems(c, changed); err != nil {
		return err
	}
	s.paintReply(c)
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
	case 4:
		// The two uint16 values are resolved against the owned decoder slot;
		// upstream does not expose a stable evaluation order.
		first, second := p.U16(), p.U16()
		if p.Err != nil {
			return p.Err
		}
		if p.Pos != len(p.Data) {
			return errors.New("unexpected item request tail")
		}
		return s.openItemBox(c, first, second)
	case 14:
		// Enhancement is not migrated yet. The client waits for this result
		// packet; SYSTEMERROR makes the dialog close instead of hanging.
		p.U8()
		if p.Err != nil {
			return p.Err
		}
		c.send(78, &protocol.Writer{Data: []byte{14, 5}})
		return nil
	case 16:
		p.U8()
		slot, days := p.U16(), p.U16()
		if p.Err != nil {
			return p.Err
		}
		if p.Pos != len(p.Data) {
			return errors.New("unexpected item extension tail")
		}
		return s.extendItem(c, slot, days)
	case 17:
		weapon, paint := p.U16(), p.U16()
		if p.Err != nil {
			return p.Err
		}
		if p.Pos != len(p.Data) {
			return errors.New("unexpected weapon paint tail")
		}
		return s.applyWeaponPaint(c, weapon, paint)
	case 24:
		weapon, paint := p.U16(), p.U16()
		if p.Err != nil {
			return p.Err
		}
		if p.Pos != len(p.Data) {
			return errors.New("unexpected weapon paint switch tail")
		}
		return s.switchWeaponPaint(c, weapon, paint)
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
