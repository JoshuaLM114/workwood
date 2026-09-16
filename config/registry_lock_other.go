//go:build !unix && !windows

package config

import "fmt"

func lockRegistry(string) (func(), error) {
	return nil, fmt.Errorf("project registry locking requires a Unix or Windows platform")
}
