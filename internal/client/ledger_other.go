//go:build !unix

package client

import (
	"errors"
	"os"
)

// The production guard needs an owner-checked, exclusively locked ledger file,
// which this implementation only provides on Unix-like systems.
func openLedger(string) (*os.File, error) {
	return nil, errors.New("production ledger requires a Unix-like operating system")
}
