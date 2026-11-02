package server

import "encoding/binary"

// This file mirrors the mode-specific cleanup in CRoomSettings::LoadNewSettings.
// Upstream silently clears or clamps optional fields that the selected mode does
// not use; the room stays valid instead of failing the request.

func (s *settings) hasField(index int) bool {
	return s.flags[index/32]&(1<<uint(index%32)) != 0
}

func (s *settings) clearField(index int) {
	s.flags[index/32] &^= 1 << uint(index%32)
	s.fields[index] = nil
}

func (s *settings) clampField(index, max int) {
	if s.number(index) > max {
		s.setNumber(index, max, 1)
	}
}

func (s *settings) setByte(index, value int) {
	s.setNumber(index, value, 1)
}

func zbLimitValues(b []byte) (int, [4]int, bool) {
	var values [4]int
	if len(b) < 17 {
		return 0, values, false
	}
	flag := int(b[0])
	for i := 0; i < 4; i++ {
		values[i] = int(binary.LittleEndian.Uint32(b[1+i*4:]))
	}
	return flag, values, true
}

func zbLimitValid(values [4]int, known func(int) bool) bool {
	empty := true
	for i := range values {
		if values[i] != 0 {
			empty = false
			break
		}
	}
	if empty {
		return true
	}
	for i, value := range values {
		if value == 0 {
			continue
		}
		if !known(value) {
			return false
		}
		for j := i + 1; j < 4; j++ {
			if value == values[j] {
				return false
			}
		}
	}
	return true
}

func mutationRestrictValid(b []byte) bool {
	if len(b) < 5 {
		return false
	}
	list := [4]int{int(b[1]), int(b[2]), int(b[3]), int(b[4])}
	empty := true
	for _, value := range list {
		if value != 255 {
			empty = false
			break
		}
	}
	if empty {
		return true
	}
	for i, value := range list {
		if value == 255 {
			continue
		}
		if value > 96 {
			return false
		}
		for j := i + 1; j < 4; j++ {
			if value == list[j] {
				return false
			}
		}
	}
	return true
}

func zbRespawnDefault(mode int) bool {
	switch mode {
	case 8, 9, 14, 20, 24, 29, 32, 45, 51, 56:
		return true
	}
	return false
}

func zbBalanceDefault(mode int) bool {
	switch mode {
	case 8, 9, 14, 20, 24, 29, 32, 45, 51:
		return true
	}
	return false
}

func weaponBuyCoolTimeAllowed(mode int) bool {
	switch mode {
	case 8, 9, 14, 24, 29, 45, 54:
		return true
	}
	return false
}

func (s *Server) sanitizeRoomSettings(st *settings, mode, mapID int) {
	// Studio settings only exist on the Voxel map.
	if mapID != 254 && st.hasField(50) {
		st.clearField(50)
	}
	for _, index := range []int{42, 39, 37} { // playlist index, status symbol, moving shot
		if st.hasField(index) {
			st.clearField(index)
		}
	}
	if st.hasField(35) {
		st.clampField(35, 1)
	}
	if st.hasField(47) {
		st.clampField(47, 2)
	}
	if st.hasField(46) { // league rule
		if mode != 0 && mode != 32 && st.number(46) != 0 {
			st.clearField(46)
		} else {
			st.clampField(46, 1)
		}
	}
	if st.hasField(49) { // zbLimit
		flag, values, ok := zbLimitValues(st.fields[49])
		switch {
		case !ok,
			mode != 9 && mode != 14 && mode != 20 && mode != 45 && mode != 54 && mode != 58 && !zbLimitEmpty(values),
			!zbLimitValid(values, func(id int) bool {
				_, known := s.items[id]
				return known
			}):
			st.clearField(49)
		default:
			if flag == 2 || flag > 3 {
				st.fields[49][0] = 3
			}
		}
	}
	if st.hasField(54) { // team switch
		if mode != 0 && mode != 3 && st.number(54) != 0 {
			st.clearField(54)
		} else {
			st.clampField(54, 1)
		}
	}
	if st.hasField(55) { // zombie respawn
		if !zbRespawnDefault(mode) && st.number(55) != 0 || mode == 56 && st.number(55) != 1 {
			st.clearField(55)
		} else {
			st.clampField(55, 1)
		}
	}
	if st.hasField(56) { // zombie balance
		if !zbBalanceDefault(mode) && mode != 56 && st.number(56) != 0 {
			st.clearField(56)
		} else {
			st.clampField(56, 1)
		}
	}
	if st.hasField(57) { // game rule
		if mode != 40 && st.number(57) != 0 {
			st.clearField(57)
		} else {
			st.clampField(57, 1)
		}
	}
	if st.hasField(60) { // zombie auto hunting
		if mode != 14 && mode != 45 && st.number(60) != 0 {
			st.clearField(60)
		} else {
			st.clampField(60, 1)
		}
	}
	if st.hasField(61) { // integrated team
		switch mode {
		case 2, 5, 56, 57:
			st.clampField(61, 1)
		default:
			if st.number(61) != 0 {
				st.clearField(61)
			}
		}
	}
	if st.hasField(43) { // enhancement restriction
		switch mode {
		case 0:
			st.clampField(43, 1)
		case 22, 32:
			if st.number(43) != 1 {
				st.clearField(43)
			}
		default:
			if st.number(43) != 0 {
				st.clearField(43)
			}
		}
	}
	if st.hasField(64) { // fire bomb
		allowed := mode == 14 || mode == 45 || mode == 55
		if !allowed && st.number(64) != 1 {
			st.clearField(64)
		} else {
			st.clampField(64, 1)
		}
	}
	if st.hasField(65) { // mutation restriction
		if mode != 45 || !mutationRestrictValid(st.fields[65]) {
			st.clearField(65)
		}
	}
	if st.hasField(66) { // mutation limit
		value := st.number(66)
		if mode != 45 && value != 20 || !mutationLimitValid(value) {
			st.clearField(66)
		}
	}
	if st.hasField(67) { // floating damage skin
		switch mode {
		case 0, 2, 3, 5:
			st.clampField(67, 1)
		default:
			if st.number(67) != 1 {
				st.clearField(67)
			}
		}
	}
	if st.hasField(68) { // player one team
		if mode != 3 && mode != 5 && st.number(68) != 0 {
			st.clearField(68)
		} else {
			st.clampField(68, 1)
		}
	}
	if st.hasField(71) { // weapon buy cool time
		if !weaponBuyCoolTimeAllowed(mode) && st.number(71) != 0 {
			st.clearField(71)
		} else {
			st.clampField(71, 1)
		}
	}
	if st.hasField(72) { // zombie rebalance
		if mode != 14 && st.number(72) != 0 {
			st.clearField(72)
		} else {
			st.clampField(72, 1)
		}
	}
	for _, index := range []int{75, 76} { // change shot, zombie revive
		if !st.hasField(index) {
			continue
		}
		if mode != 58 && st.number(index) != 0 {
			st.clearField(index)
		} else {
			st.clampField(index, 1)
		}
	}
}

func zbLimitEmpty(values [4]int) bool {
	for _, value := range values {
		if value != 0 {
			return false
		}
	}
	return true
}

func mutationLimitValid(value int) bool {
	switch value {
	case 20, 25, 30, 35, 40:
		return true
	}
	return false
}
