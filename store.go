package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *containerStore) init() error {
	if err := os.MkdirAll(filepath.Join(s.stateDir, "containers"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.stateDir, "networks"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.stateDir, "images"), 0o755); err != nil {
		return err
	}
	if err := s.loadAll(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureDefaultNetworkLocked()
}

func (s *containerStore) loadAll() error {
	entries, err := os.ReadDir(filepath.Join(s.stateDir, "containers"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.stateDir, "containers", entry.Name()))
		if err != nil {
			continue
		}
		var c Container
		if err := json.Unmarshal(data, &c); err != nil {
			continue
		}
		if !isSafeStateID(c.ID) || entry.Name() != c.ID+".json" {
			continue
		}
		containerDir := filepath.Join(s.stateDir, "containers", c.ID)
		if ok, _ := isPathWithinBase(containerDir, c.Rootfs); !ok {
			continue
		}
		rootfsInfo, err := os.Lstat(c.Rootfs)
		if err != nil || !rootfsInfo.IsDir() {
			continue
		}
		if err := isDirSafe(containerDir, c.Rootfs); err != nil {
			continue
		}
		s.containers[c.ID] = &c
	}
	return s.loadNetworks()
}

func (s *containerStore) containerPath(id string) string {
	return filepath.Join(s.stateDir, "containers", id+".json")
}

func (s *containerStore) saveContainer(c *Container) error {
	if c == nil || !isSafeStateID(c.ID) {
		return fmt.Errorf("invalid container id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWriteFile(s.containerPath(c.ID), data, 0o600); err != nil {
		return err
	}
	s.containers[c.ID] = c
	return nil
}

func isSafeStateID(id string) bool {
	if id == "" || id != strings.TrimSpace(id) || id == "." || id == ".." || filepath.Base(id) != id {
		return false
	}
	for _, ch := range id {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
}

func (s *containerStore) findContainer(id string) (*Container, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findContainerLocked(id)
}

func (s *containerStore) findContainerForOwner(id, owner string) (*Container, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.findContainerLocked(id)
	if !ok || c == nil || !ownerMatches(c.Owner, owner) {
		return nil, false
	}
	return c, true
}

func (s *containerStore) findContainerLocked(id string) (*Container, bool) {
	id = normalizeContainerName(id)
	if id == "" {
		return nil, false
	}
	if c, ok := s.containers[id]; ok {
		return c, true
	}
	var nameMatch *Container
	for _, c := range s.containers {
		if c.Name != "" && normalizeContainerName(c.Name) == id {
			if nameMatch != nil && nameMatch.ID != c.ID {
				return nil, false
			}
			nameMatch = c
		}
	}
	if nameMatch != nil {
		return nameMatch, true
	}
	var prefixMatch *Container
	for containerID, c := range s.containers {
		if strings.HasPrefix(containerID, id) {
			if prefixMatch != nil && prefixMatch.ID != c.ID {
				return nil, false
			}
			prefixMatch = c
		}
	}
	return prefixMatch, prefixMatch != nil
}

// beginContainerStart serializes starts for a container. The returned already
// flag is true when the container is running or another start is in progress.
func (s *containerStore) beginContainerStart(ref string) (c *Container, found, already bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, found = s.findContainerLocked(ref)
	if !found || c == nil {
		return nil, false, false
	}
	if c.Running {
		return c, true, true
	}
	if s.starting == nil {
		s.starting = make(map[string]struct{})
	}
	if _, ok := s.starting[c.ID]; ok {
		return c, true, true
	}
	s.starting[c.ID] = struct{}{}
	return c, true, false
}

func (s *containerStore) endContainerStart(id string) {
	s.mu.Lock()
	delete(s.starting, id)
	s.mu.Unlock()
}

func (s *containerStore) markStopped(id string) {
	finishedAt := time.Now().UTC()
	s.markStoppedWithExit(id, nil, finishedAt)
}

func (s *containerStore) markStoppedWithExit(id string, exitCode *int, finishedAt time.Time) {
	s.mu.Lock()
	c, ok := s.containers[id]
	if ok {
		c.Running = false
		c.Pid = 0
		if exitCode != nil {
			c.ExitCode = *exitCode
		}
		if !finishedAt.IsZero() {
			c.FinishedAt = finishedAt
		}
		_ = s.saveLocked(c)
	}
	s.mu.Unlock()
}

func (s *containerStore) saveLocked(c *Container) error {
	if c == nil || !isSafeStateID(c.ID) {
		return fmt.Errorf("invalid container id")
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.containerPath(c.ID), data, 0o600)
}

func (s *containerStore) listContainers() []map[string]interface{} {
	return s.listContainersForOwner("")
}

func (s *containerStore) listContainersForOwner(owner string) []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]interface{}, 0, len(s.containers))
	for _, c := range s.containers {
		if owner != "" && !ownerMatches(c.Owner, owner) {
			continue
		}
		out = append(out, map[string]interface{}{
			"Id":      c.ID,
			"Image":   c.Image,
			"Command": strings.Join(c.Cmd, " "),
			"Created": c.Created.Unix(),
			"State":   statusFromRunning(c.Running),
			"Status":  statusFromRunning(c.Running),
			"Ports":   toDockerPortSummaries(c.Ports),
			"Names":   []string{containerDisplayName(c)},
		})
	}
	return out
}

func (s *containerStore) listContainerIDs() []string {
	return s.listContainerIDsForOwner("")
}

func (s *containerStore) listContainerIDsForOwner(owner string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.containers))
	for id, c := range s.containers {
		if owner != "" && !ownerMatches(c.Owner, owner) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (s *containerStore) hasK8sContainersForOwner(owner, namespace string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.containers {
		if c == nil || strings.TrimSpace(c.K8sPodName) == "" || !ownerMatches(c.Owner, owner) {
			continue
		}
		if namespace == "" || strings.TrimSpace(c.K8sNamespace) == "" || c.K8sNamespace == namespace {
			return true
		}
	}
	return false
}

func (s *containerStore) runningCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, c := range s.containers {
		if c != nil && c.Running {
			count++
		}
	}
	return count
}

func normalizeContainerName(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(raw), "/")
}

func containerDisplayName(c *Container) string {
	name := normalizeContainerName(c.Name)
	if name == "" {
		name = c.ID
	}
	return "/" + name
}

func containerTmpDir(c *Container) string {
	if c == nil {
		return "/tmp"
	}
	return filepath.Join(filepath.Dir(c.Rootfs), "tmp")
}

func (s *containerStore) nameInUse(raw string) bool {
	name := normalizeContainerName(raw)
	if name == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.containers {
		if normalizeContainerName(c.Name) == name {
			return true
		}
	}
	return false
}

func (s *containerStore) setProxies(id string, proxies []*portProxy) {
	s.mu.Lock()
	s.proxies[id] = proxies
	s.mu.Unlock()
}

func (s *containerStore) stopProxies(id string) {
	s.mu.Lock()
	proxies := s.proxies[id]
	delete(s.proxies, id)
	s.mu.Unlock()
	for _, proxy := range proxies {
		proxy.stopProxy()
	}
}

func (s *containerStore) setSSHCompat(id string, server *sshCompatServer) {
	if server == nil {
		return
	}
	s.mu.Lock()
	if s.sshCompat == nil {
		s.sshCompat = make(map[string]*sshCompatServer)
	}
	s.sshCompat[id] = server
	s.mu.Unlock()
}

func (s *containerStore) stopSSHCompat(id string) {
	s.mu.Lock()
	server := s.sshCompat[id]
	delete(s.sshCompat, id)
	s.mu.Unlock()
	if server != nil {
		server.stop()
	}
}

func (s *containerStore) putExec(inst *ExecInstance) {
	s.mu.Lock()
	s.execs[inst.ID] = inst
	s.mu.Unlock()
}

func (s *containerStore) findExec(id string) (*ExecInstance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.execs[id]
	return inst, ok
}

// Backward-compat wrappers during naming migration.
func (s *containerStore) save(c *Container) error { return s.saveContainer(c) }

func (s *containerStore) get(id string) (*Container, bool) { return s.findContainer(id) }

func (s *containerStore) saveExec(inst *ExecInstance) { s.putExec(inst) }

func (s *containerStore) getExec(id string) (*ExecInstance, bool) { return s.findExec(id) }

func (s *containerStore) stopAllRunning(grace time.Duration) int {
	type stopTarget struct {
		id         string
		running    bool
		pid        int
		k8sManaged bool
	}

	s.mu.Lock()
	targets := make([]stopTarget, 0, len(s.containers))
	extraProxyIDs := make([]string, 0, len(s.proxies))
	extraSSHCompatIDs := make([]string, 0, len(s.sshCompat))
	for _, c := range s.containers {
		targets = append(targets, stopTarget{
			id:         c.ID,
			running:    c.Running,
			pid:        c.Pid,
			k8sManaged: strings.TrimSpace(c.K8sPodName) != "",
		})
	}
	for id := range s.proxies {
		extraProxyIDs = append(extraProxyIDs, id)
	}
	for id := range s.sshCompat {
		extraSSHCompatIDs = append(extraSSHCompatIDs, id)
	}
	s.mu.Unlock()

	stopped := 0
	knownIDs := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		knownIDs[target.id] = struct{}{}
		s.stopProxies(target.id)
		s.stopSSHCompat(target.id)
		if target.running {
			if target.pid > 0 {
				terminateProcessTree(target.pid, grace)
				s.markStopped(target.id)
				stopped++
				continue
			}
			if !target.k8sManaged {
				s.markStopped(target.id)
				stopped++
			}
		}
	}
	for _, id := range extraProxyIDs {
		if _, ok := knownIDs[id]; ok {
			continue
		}
		s.stopProxies(id)
	}
	for _, id := range extraSSHCompatIDs {
		if _, ok := knownIDs[id]; ok {
			continue
		}
		s.stopSSHCompat(id)
	}
	return stopped
}

func (s *containerStore) allocateLoopbackIP() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	used := map[string]struct{}{}
	for _, c := range s.containers {
		if ip := strings.TrimSpace(c.LoopbackIP); ip != "" {
			used[ip] = struct{}{}
		}
	}
	for octet := 2; octet <= 254; octet++ {
		ip := fmt.Sprintf("127.0.0.%d", octet)
		if _, ok := used[ip]; ok {
			continue
		}
		return ip, nil
	}
	return "", fmt.Errorf("no loopback ip available")
}
