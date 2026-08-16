package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const builtInBridgeNetworkID = "bridge"

func (s *containerStore) ensureNetworksMapLocked() {
	if s.networks == nil {
		s.networks = make(map[string]*Network)
	}
}

func (s *containerStore) networkPath(id string) string {
	return filepath.Join(s.stateDir, "networks", id+".json")
}

func (s *containerStore) loadNetworks() error {
	s.ensureNetworksMapLocked()
	entries, err := os.ReadDir(filepath.Join(s.stateDir, "networks"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.stateDir, "networks", entry.Name()))
		if err != nil {
			continue
		}
		var n Network
		if err := json.Unmarshal(data, &n); err != nil {
			continue
		}
		if !isSafeStateID(n.ID) || entry.Name() != n.ID+".json" {
			continue
		}
		if n.Containers == nil {
			n.Containers = map[string]*NetworkEndpoint{}
		}
		s.networks[n.ID] = &n
	}
	return nil
}

func (s *containerStore) ensureDefaultNetworkLocked() error {
	s.ensureNetworksMapLocked()
	if _, ok := s.networks[builtInBridgeNetworkID]; ok {
		return nil
	}
	n := &Network{
		ID:         builtInBridgeNetworkID,
		Name:       "bridge",
		Driver:     "bridge",
		Scope:      "local",
		Created:    time.Now().UTC().Format(time.RFC3339Nano),
		Containers: map[string]*NetworkEndpoint{},
		IPAM: map[string]interface{}{
			"Driver":  "default",
			"Config":  []interface{}{},
			"Options": map[string]string{},
		},
		Options: map[string]string{},
		Labels:  map[string]string{},
	}
	s.networks[n.ID] = n
	return s.saveNetworkLocked(n)
}

func (s *containerStore) saveNetworkLocked(n *Network) error {
	s.ensureNetworksMapLocked()
	if n == nil || !isSafeStateID(n.ID) {
		return fmt.Errorf("invalid network id")
	}
	data, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.networkPath(n.ID), data, 0o600)
}

func (s *containerStore) upsertNetwork(n *Network) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	if err := s.saveNetworkLocked(n); err != nil {
		return err
	}
	s.networks[n.ID] = n
	return nil
}

func (s *containerStore) findNetwork(ref string) (*Network, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	return s.findNetworkLocked(ref)
}

func (s *containerStore) findNetworkForOwner(ref, owner string) (*Network, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	n, ok := s.findNetworkLocked(ref)
	if !ok || n == nil {
		return nil, false
	}
	if n.ID != builtInBridgeNetworkID && !ownerMatches(n.Owner, owner) {
		return nil, false
	}
	return n, true
}

func (s *containerStore) findNetworkLocked(ref string) (*Network, bool) {
	ref = normalizeContainerName(ref)
	if ref == "" {
		return nil, false
	}
	if n, ok := s.networks[ref]; ok {
		return n, true
	}
	var nameMatch *Network
	for _, n := range s.networks {
		if strings.EqualFold(n.Name, ref) {
			if nameMatch != nil && nameMatch.ID != n.ID {
				return nil, false
			}
			nameMatch = n
		}
	}
	if nameMatch != nil {
		return nameMatch, true
	}
	var prefixMatch *Network
	for id, n := range s.networks {
		if strings.HasPrefix(id, ref) {
			if prefixMatch != nil && prefixMatch.ID != n.ID {
				return nil, false
			}
			prefixMatch = n
		}
	}
	return prefixMatch, prefixMatch != nil
}

func (s *containerStore) listNetworks() []map[string]interface{} {
	return s.listNetworksForOwner("")
}

func (s *containerStore) listNetworksForOwner(owner string) []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	out := make([]map[string]interface{}, 0, len(s.networks))
	ids := make([]string, 0, len(s.networks))
	for id, n := range s.networks {
		if owner != "" && id != builtInBridgeNetworkID && !ownerMatches(n.Owner, owner) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := s.networks[id]
		out = append(out, map[string]interface{}{
			"Name":       n.Name,
			"Id":         n.ID,
			"Created":    n.Created,
			"Scope":      firstNonEmpty(n.Scope, "local"),
			"Driver":     firstNonEmpty(n.Driver, "bridge"),
			"EnableIPv6": n.EnableIPv6,
			"Internal":   n.Internal,
			"Attachable": n.Attachable,
			"Ingress":    n.Ingress,
			"Labels":     n.Labels,
		})
	}
	return out
}

func (s *containerStore) networkViewForOwner(n *Network, owner string) *Network {
	if n == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view := *n
	view.Containers = make(map[string]*NetworkEndpoint)
	for id, endpoint := range n.Containers {
		c := s.containers[id]
		if c == nil || !ownerMatches(c.Owner, owner) {
			continue
		}
		view.Containers[id] = endpoint
	}
	return &view
}

// Backward-compat wrappers during naming migration.
func (s *containerStore) saveNetwork(n *Network) error { return s.upsertNetwork(n) }

func (s *containerStore) getNetwork(ref string) (*Network, bool) { return s.findNetwork(ref) }

func (s *containerStore) connectContainerToNetwork(networkID string, c *Container, aliases []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	n, ok := s.networks[networkID]
	if !ok || c == nil {
		return nil
	}
	if n.Containers == nil {
		n.Containers = map[string]*NetworkEndpoint{}
	}
	ep := n.Containers[c.ID]
	if ep == nil {
		endpointID, err := randomID(12)
		if err != nil {
			return err
		}
		ep = &NetworkEndpoint{
			Name:     normalizeContainerName(c.Name),
			Endpoint: endpointID,
			Aliases:  []string{},
		}
	}
	if ep.Name == "" {
		ep.Name = c.ID
	}
	aliasSet := map[string]struct{}{}
	for _, a := range ep.Aliases {
		if a = strings.TrimSpace(a); a != "" {
			aliasSet[a] = struct{}{}
		}
	}
	for _, a := range aliases {
		if a = strings.TrimSpace(a); a != "" {
			aliasSet[a] = struct{}{}
		}
	}
	if c.Hostname != "" {
		aliasSet[c.Hostname] = struct{}{}
	}
	if ep.Name != "" {
		aliasSet[ep.Name] = struct{}{}
	}
	ep.Aliases = ep.Aliases[:0]
	for a := range aliasSet {
		ep.Aliases = append(ep.Aliases, a)
	}
	sort.Strings(ep.Aliases)
	n.Containers[c.ID] = ep
	return s.saveNetworkLocked(n)
}

func (s *containerStore) disconnectContainerFromNetwork(networkID, containerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	n, ok := s.networks[networkID]
	if !ok {
		return nil
	}
	delete(n.Containers, containerID)
	return s.saveNetworkLocked(n)
}

func (s *containerStore) disconnectContainerFromAllNetworks(containerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	for _, n := range s.networks {
		if n.Containers == nil {
			continue
		}
		if _, ok := n.Containers[containerID]; !ok {
			continue
		}
		delete(n.Containers, containerID)
		_ = s.saveNetworkLocked(n)
	}
}

func (s *containerStore) networkSettingsForContainer(containerID string) map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	out := map[string]interface{}{}
	for _, n := range s.networks {
		ep, ok := n.Containers[containerID]
		if !ok {
			continue
		}
		out[n.Name] = map[string]interface{}{
			"NetworkID":           n.ID,
			"EndpointID":          ep.Endpoint,
			"Gateway":             "",
			"IPAddress":           "",
			"IPPrefixLen":         0,
			"IPv6Gateway":         "",
			"GlobalIPv6Address":   "",
			"GlobalIPv6PrefixLen": 0,
			"MacAddress":          ep.Mac,
			"Aliases":             ep.Aliases,
		}
	}
	return out
}

func (s *containerStore) peerAliasesForContainer(containerID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	owner := anonymousOwner
	if c := s.containers[containerID]; c != nil {
		owner = canonicalOwner(c.Owner)
	}
	aliases := map[string]struct{}{}
	for _, n := range s.networks {
		if _, attached := n.Containers[containerID]; !attached {
			continue
		}
		for peerID, ep := range n.Containers {
			if peerID == containerID || ep == nil {
				continue
			}
			peer := s.containers[peerID]
			if peer == nil || !ownerMatches(peer.Owner, owner) {
				continue
			}
			for _, alias := range ep.Aliases {
				if alias = strings.TrimSpace(alias); alias != "" {
					aliases[alias] = struct{}{}
				}
			}
			if ep.Name != "" {
				aliases[ep.Name] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(aliases))
	for alias := range aliases {
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func (s *containerStore) containersSharingNetworks(containerID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	owner := anonymousOwner
	if c := s.containers[containerID]; c != nil {
		owner = canonicalOwner(c.Owner)
	}
	ids := map[string]struct{}{}
	for _, n := range s.networks {
		if _, attached := n.Containers[containerID]; !attached {
			continue
		}
		for peerID := range n.Containers {
			if strings.TrimSpace(peerID) == "" {
				continue
			}
			peer := s.containers[peerID]
			if peer == nil || !ownerMatches(peer.Owner, owner) {
				continue
			}
			ids[peerID] = struct{}{}
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (s *containerStore) peerHostAliasesForContainer(containerID string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	owner := anonymousOwner
	if c := s.containers[containerID]; c != nil {
		owner = canonicalOwner(c.Owner)
	}
	out := map[string]string{}
	for _, n := range s.networks {
		if _, attached := n.Containers[containerID]; !attached {
			continue
		}
		for peerID, ep := range n.Containers {
			if peerID == containerID || ep == nil {
				continue
			}
			peer := s.containers[peerID]
			if peer == nil || !ownerMatches(peer.Owner, owner) {
				continue
			}
			ip := strings.TrimSpace(peer.K8sPodIP)
			if ip == "" {
				if isZookeeperImage(peer.Image) || isZookeeperImage(peer.ResolvedImage) {
					// Zookeeper images in proot bind loopback-local by default.
					// Keep aliases on 127.0.0.1 for peer connectivity.
					ip = "127.0.0.1"
				} else {
					ip = strings.TrimSpace(peer.LoopbackIP)
				}
			}
			if ip == "" {
				continue
			}
			for _, alias := range ep.Aliases {
				alias = normalizeContainerHostname(alias)
				if alias == "" {
					continue
				}
				if _, ok := out[alias]; !ok {
					out[alias] = ip
				}
			}
			name := normalizeContainerHostname(ep.Name)
			if name != "" {
				if _, ok := out[name]; !ok {
					out[name] = ip
				}
			}
		}
	}
	return out
}

func (s *containerStore) selfHostAliasesForContainer(containerID string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureNetworksMapLocked()
	selfIP := "127.0.0.1"
	if c := s.containers[containerID]; c != nil {
		if ip := strings.TrimSpace(c.K8sPodIP); ip != "" {
			selfIP = ip
		} else if ip := strings.TrimSpace(c.LoopbackIP); ip != "" {
			selfIP = ip
		}
	}
	out := map[string]string{}
	for _, n := range s.networks {
		ep, attached := n.Containers[containerID]
		if !attached || ep == nil {
			continue
		}
		for _, alias := range ep.Aliases {
			alias = normalizeContainerHostname(alias)
			if alias == "" {
				continue
			}
			out[alias] = selfIP
		}
		name := normalizeContainerHostname(ep.Name)
		if name != "" {
			out[name] = selfIP
		}
	}
	return out
}
