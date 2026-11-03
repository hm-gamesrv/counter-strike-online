package store

// HasActiveItem reports whether the account owns a row for itemID that is
// selected and active. The reference settles the class bonus with exactly this
// lookup (GetFirstActiveItemByItemID: status=1 AND inUse=1), so an expired row
// that the sweep already removed cannot keep earning the bonus. The query
// deliberately has no expiry or count filter, matching the reference.
func (s *Store) HasActiveItem(id, itemID int) (bool, error) {
	if id <= 0 || itemID <= 0 {
		return false, nil
	}
	var found int
	err := s.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM UserInventory WHERE userID=? AND itemID=? AND status=1 AND inUse=1)", id, itemID).Scan(&found)
	if err != nil {
		return false, err
	}
	return found != 0, nil
}
