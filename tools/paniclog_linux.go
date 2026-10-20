// Log the panic under unix to the log file

//go:build linux
// +build linux

package tools

import (
	"golang.org/x/sys/unix"
	"log"
	"os"
)

// redirectStderr to the file passed in
func RedirectStderr(f *os.File) {
	err := unix.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
	if err != nil {
		log.Printf("Failed to redirect stderr to file: %v", err)
	}
}
