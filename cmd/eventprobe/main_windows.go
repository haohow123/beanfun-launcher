//go:build windows

// Package main is the eventprobe one-off diagnostic tool. It watches
// a target Win32 window's UI events via SetWinEventHook and streams
// them to stdout, so we can pick the right event to subscribe to in
// M10's 1-click 啟動+帶入 auto-inject feature.
//
// Not part of the production launcher. Build with
//
//	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o eventprobe.exe ./cmd/eventprobe
//
// See cmd/eventprobe/README.md for usage.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func main() {
	// SetWinEventHook with WINEVENT_OUTOFCONTEXT requires the
	// installing thread to also pump messages — lock main()'s
	// goroutine to a single OS thread so both happen on the same
	// thread.
	runtime.LockOSThread()

	var (
		className string
		pidFlag   uint
	)
	flag.StringVar(&className, "class", "", "window class name to find (FindWindowW first match)")
	flag.UintVar(&pidFlag, "pid", 0, "target process ID (enumerate windows for the first matching top-level)")
	flag.Parse()

	hwnd, pid, err := findTargetWindow(className, uint32(pidFlag))
	if err != nil {
		log.Fatalf("eventprobe: %v", err)
	}

	cls := getWindowClass(hwnd)
	fmt.Printf("eventprobe — watching hwnd=0x%X pid=%d class=%q\n", hwnd, pid, cls)
	fmt.Println("Press Ctrl-C to stop.")
	fmt.Println()

	start := time.Now()
	cb := func(event uint32, h uintptr, idObject, idChild int32) {
		elapsed := time.Since(start)
		childCls := getWindowClass(h)
		fmt.Printf("T+%7.3fs  %-30s hwnd=0x%X  obj=%d  child=%d  class=%q\n",
			elapsed.Seconds(), eventName(event), h, idObject, idChild, childCls)
	}

	uninstall, err := installEventHook(pid, cb)
	if err != nil {
		log.Fatalf("eventprobe: install hook: %v", err)
	}
	defer uninstall()

	// Forward Ctrl-C / SIGTERM into a WM_QUIT message on the
	// hook-installation thread so runMessageLoop returns cleanly.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(os.Stderr, "\nstopping…")
		postQuit()
	}()

	// runMessageLoop blocks until WM_QUIT (posted by signal handler
	// above) or some fatal GetMessage error. It returns when the
	// message pump exits.
	runMessageLoop(nil)
}
