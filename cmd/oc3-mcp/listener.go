package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"
)

// listenDelegated opens the Unix socket of the MCP. A stale socket left by a
// stopped process is replaced; a live socket or any other file at the path is
// refused. Access is controlled by the directory permissions and the socket
// group; the socket itself is restricted to owner and group.
func listenDelegated(path string) (net.Listener, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode().Type() != fs.ModeSocket:
		return nil, fmt.Errorf("delegated socket path %s exists and is not a socket", path)
	case err == nil:
		if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
			conn.Close()
			return nil, fmt.Errorf("delegated socket %s is already in use", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale delegated socket %s: %w", path, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("inspect delegated socket path %s: %w", path, err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on delegated socket %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		listener.Close()
		return nil, fmt.Errorf("restrict delegated socket %s: %w", path, err)
	}
	return listener, nil
}
