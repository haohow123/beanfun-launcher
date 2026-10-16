package beanfun

// SessionAccess hands the launcher the live sessions without making them Wails-bound methods.
type SessionAccess struct{ s *LoginService }

func NewSessionAccess(s *LoginService) SessionAccess { return SessionAccess{s: s} }

func (a SessionAccess) Snapshot(loginID string) (*BeanfunClient, *Session) {
	return a.s.snapshot(loginID)
}

func (a SessionAccess) EndSessionIfCurrent(loginID string, c *BeanfunClient) bool {
	return a.s.endSessionIfCurrent(loginID, c)
}

// HasLogin reports whether at least one login is held.
func (a SessionAccess) HasLogin() bool { return a.s.hasLogin() }
