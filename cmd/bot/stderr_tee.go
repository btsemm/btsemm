// stderr_tee.go: tee fd 2 (stderr) to both the original terminal and a writer.
//
// Go's runtime writes panic traces and SIGQUIT goroutine dumps directly to
// fd 2, NOT through the `log` package or the os.Stderr variable — so reassigning
// os.Stderr or calling log.SetOutput would not capture them. The only way to
// intercept those writes is at the file-descriptor level: dup the original
// fd 2, replace fd 2 with a pipe's write end, then read from the pipe in a
// goroutine and tee to (original stderr, sink).
//
// This is POSIX-only (uses syscall.Dup / syscall.Dup2). The bot is Linux-only
// in practice; if a Windows build is ever needed, gate this with build tags
// and provide a no-op fallback.

package main

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// teeStderr makes everything written to fd 2 appear on the original terminal
// stderr AND on `sink`. Returns a close function that should be deferred to
// drain the pipe at shutdown. Errors are returned for the caller to log; the
// function is best-effort and the caller should continue on failure.
func teeStderr(sink io.Writer) (func(), error) {
	// Save the original stderr fd so terminal output continues unchanged.
	origFD, err := syscall.Dup(int(os.Stderr.Fd()))
	if err != nil {
		return func() {}, fmt.Errorf("dup stderr: %w", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		syscall.Close(origFD)
		return func() {}, fmt.Errorf("pipe: %w", err)
	}

	// Replace fd 2 with the pipe's write end. Subsequent writes by the Go
	// runtime, panics, SIGQUIT dumps, cgo libraries, etc. all flow into the
	// pipe.
	if err := syscall.Dup2(int(w.Fd()), int(os.Stderr.Fd())); err != nil {
		syscall.Close(origFD)
		r.Close()
		w.Close()
		return func() {}, fmt.Errorf("dup2 stderr: %w", err)
	}
	// fd 2 now references the pipe; we can drop our extra os.File handle.
	w.Close()

	origStderr := os.NewFile(uintptr(origFD), "/dev/stderr")
	done := make(chan struct{})

	go func() {
		defer close(done)
		// Tee everything from the pipe to (real terminal stderr, log sink).
		// io.Copy returns when r is closed or hits EOF (which only happens
		// after the last writer to fd 2 closes — i.e., process exit). At
		// shutdown, the close func below will close r and unblock us.
		_, _ = io.Copy(io.MultiWriter(origStderr, sink), r)
	}()

	closeFn := func() {
		// Closing the read end causes the goroutine's io.Copy to return.
		// We don't try to close fd 2 — the runtime owns it now and the OS
		// will reap it on process exit.
		r.Close()
		<-done
		origStderr.Close()
	}
	return closeFn, nil
}
