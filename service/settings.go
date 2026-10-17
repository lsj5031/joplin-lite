package service

// Setting returns a stored setting ("" when unset). Values are opaque
// strings — Joplin stores settings as JSON values in its settings table, so
// the caller owns the encoding.
func (s *Service) Setting(key string) (string, error) {
	return s.db.GetSetting(key)
}

// SetSetting stores a setting (sync target, clipper token, etc.). Secrets
// such as the master password are set through the e2ee layer at M4, not
// through this seam.
func (s *Service) SetSetting(key, value string) error {
	return s.db.SetSetting(key, value)
}
