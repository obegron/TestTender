package types

import (
	"regexp"
	"time"
)

// Network describes the details of a network.
type Network struct {
	ID      string
	ShortID string
	Name    string
	Driver  string
	Options map[string]string
	Labels  map[string]string
	Created time.Time
}

// DriverName returns the Docker compatibility metadata for a network. The
// value does not select a Kubernetes CNI or alter Pod networking.
func (nw *Network) DriverName() string {
	if nw.Driver == "" {
		return "bridge"
	}
	return nw.Driver
}

// IsPredefined will return if the network is a pre-defined system network.
func (nw *Network) IsPredefined() bool {
	return nw.Name == "bridge" || nw.Name == "null" || nw.Name == "host"
}

// Match will match given type with given key value pair.
func (nw *Network) Match(typ string, key string, val string) (bool, error) {
	if typ == "name" {
		return nw.nameMatch(key)
	}
	if typ != "label" {
		return true, nil
	}
	v, ok := nw.Labels[key]
	if !ok {
		return false, nil
	}
	return v == val, nil
}

func (nw *Network) nameMatch(key string) (bool, error) {
	// Fast path, exact match
	if nw.Name == key {
		return true, nil
	}
	// Fallback to regexp
	match, err := regexp.MatchString(key, nw.Name)
	if err != nil {
		return false, err
	}
	if match {
		return true, nil
	}
	return false, nil
}
