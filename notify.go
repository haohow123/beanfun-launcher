package main

import "errors"

// Shared constants for the server-online toast, plus the non-Windows error
// (notify_other.go returns it; notify_windows.go never does).
const (
	serverOnlineID    = "maple-server-online"
	serverOnlineTitle = "新楓之谷 MapleStory"
	serverOnlineBody  = "伺服器已開啟"
)

var errToastUnsupported = errors.New("toast not supported on this platform")
