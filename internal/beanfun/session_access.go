package beanfun

// SessionAccess hands the launcher the live session without making it a Wails-bound method.
type SessionAccess struct{ s *LoginService }

func NewSessionAccess(s *LoginService) SessionAccess { return SessionAccess{s: s} }

func (a SessionAccess) Snapshot() (*BeanfunClient, *Session) { return a.s.snapshot() }

func (a SessionAccess) EndSessionIfCurrent(c *BeanfunClient) bool { return a.s.endSessionIfCurrent(c) }
