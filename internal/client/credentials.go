package client

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const maxCredentialsFileBytes = 64 << 10

// BasicCredentials is the collector account the MCP signs in with until the
// collector accepts the delegated token of the caller.
type BasicCredentials struct {
	Login    string
	Password string
}

// LoadBasicCredentials reads the login and password for host from a netrc
// file readable by its owner only. The file is read once, at startup.
func LoadBasicCredentials(path string, host string) (BasicCredentials, error) {
	file, err := os.Open(path)
	if err != nil {
		return BasicCredentials{}, fmt.Errorf("open collector credentials file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return BasicCredentials{}, fmt.Errorf("inspect collector credentials file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return BasicCredentials{}, fmt.Errorf("collector credentials file must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return BasicCredentials{}, fmt.Errorf("collector credentials file must not be accessible by group or others, mode is %04o", info.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCredentialsFileBytes+1))
	if err != nil {
		return BasicCredentials{}, fmt.Errorf("read collector credentials file: %w", err)
	}
	if len(data) > maxCredentialsFileBytes {
		return BasicCredentials{}, fmt.Errorf("collector credentials file exceeds %d bytes", maxCredentialsFileBytes)
	}
	return parseNetrc(string(data), host)
}

// parseNetrc returns the entry of the machine host, or the default entry.
// Errors never quote the file content.
func parseNetrc(content string, host string) (BasicCredentials, error) {
	fields := strings.Fields(content)
	var found, fallback *BasicCredentials
	var current *BasicCredentials
	currentMatches, currentDefault := false, false
	closeEntry := func() {
		if current == nil {
			return
		}
		if currentMatches && found == nil {
			found = current
		}
		if currentDefault && fallback == nil {
			fallback = current
		}
	}
	for index := 0; index < len(fields); index++ {
		switch fields[index] {
		case "machine":
			if index+1 >= len(fields) {
				return BasicCredentials{}, fmt.Errorf("collector credentials file has a machine without a name")
			}
			closeEntry()
			index++
			current, currentMatches, currentDefault = &BasicCredentials{}, strings.EqualFold(fields[index], host), false
		case "default":
			closeEntry()
			current, currentMatches, currentDefault = &BasicCredentials{}, false, true
		case "login", "password", "account":
			if current == nil || index+1 >= len(fields) {
				return BasicCredentials{}, fmt.Errorf("collector credentials file has a %s outside a machine entry or without a value", fields[index])
			}
			index++
			switch fields[index-1] {
			case "login":
				current.Login = fields[index]
			case "password":
				current.Password = fields[index]
			}
		case "macdef":
			return BasicCredentials{}, fmt.Errorf("collector credentials file must not define macros")
		default:
			return BasicCredentials{}, fmt.Errorf("collector credentials file has an unexpected token")
		}
	}
	closeEntry()
	entry := found
	if entry == nil {
		entry = fallback
	}
	if entry == nil {
		return BasicCredentials{}, fmt.Errorf("collector credentials file has no entry for machine %s", host)
	}
	if entry.Login == "" || entry.Password == "" {
		return BasicCredentials{}, fmt.Errorf("collector credentials file entry for machine %s needs a login and a password", host)
	}
	return *entry, nil
}
