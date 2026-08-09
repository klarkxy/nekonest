package adapters

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// RecentSessionWindow is the fixed phone-visible native-thread window.
const RecentSessionWindow = 7 * 24 * time.Hour

// attentionProbeBytes bounds each end of an old JSONL file that discovery
// examines on a cold cache. Session identity is stored near the head while an
// unresolved attention transition is necessarily at the live tail.
const attentionProbeBytes int64 = 256 * 1024

// Match the adapters' existing scanner ceiling only for the rare case where
// the first or last JSONL record itself exceeds the normal probe window.
const attentionProbeRecordLimit int64 = 10 * 1024 * 1024

func recentSessionCutoff(now time.Time) time.Time {
	return now.Add(-RecentSessionWindow)
}

func sessionNeedsAttention(status AgentStatus) bool {
	switch status {
	case StatusRunning, StatusWaitingApproval, StatusWaitingUser:
		return true
	default:
		return false
	}
}

func sessionIsVisible(session *SessionInfo, now time.Time) bool {
	return session != nil &&
		(sessionNeedsAttention(session.Status) || !session.LastActivity.Before(recentSessionCutoff(now)))
}

type fileFingerprint struct {
	size           int64
	modifiedUnixNS int64
}

type discoveryCacheEntry[T any] struct {
	fingerprint fileFingerprint
	value       T
	err         error
}

// fileDiscoveryCache caches compact discovery metadata, never transcript bodies.
// Each adapter owns an instance while sharing the same fingerprint/invalidation
// semantics. The normalized absolute path is the cache key.
type fileDiscoveryCache[T any] struct {
	mu      sync.Mutex
	entries map[string]discoveryCacheEntry[T]
	hits    uint64
	misses  uint64
}

func newFileDiscoveryCache[T any]() *fileDiscoveryCache[T] {
	return &fileDiscoveryCache[T]{entries: make(map[string]discoveryCacheEntry[T])}
}

func normalizedDiscoveryPath(path string) string {
	path = filepath.Clean(path)
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func fingerprintFor(info os.FileInfo) fileFingerprint {
	return fileFingerprint{size: info.Size(), modifiedUnixNS: info.ModTime().UnixNano()}
}

func (c *fileDiscoveryCache[T]) load(
	path string,
	info os.FileInfo,
	parse func() (T, error),
) (T, error) {
	key := normalizedDiscoveryPath(path)
	fingerprint := fingerprintFor(info)
	c.mu.Lock()
	if entry, ok := c.entries[key]; ok && entry.fingerprint == fingerprint {
		c.hits++
		c.mu.Unlock()
		return entry.value, entry.err
	}
	c.misses++
	c.mu.Unlock()

	value, err := parse()
	c.mu.Lock()
	c.entries[key] = discoveryCacheEntry[T]{fingerprint: fingerprint, value: value, err: err}
	c.mu.Unlock()
	return value, err
}

func (c *fileDiscoveryCache[T]) peek(path string, info os.FileInfo) (T, error, bool) {
	key := normalizedDiscoveryPath(path)
	fingerprint := fingerprintFor(info)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.fingerprint != fingerprint {
		var zero T
		return zero, nil, false
	}
	return entry.value, entry.err, true
}

// prune removes deleted files and entries that have aged outside discovery.
// Callers add only currently eligible files to keep, so old records do not
// accumulate after the visible window moves forward.
func (c *fileDiscoveryCache[T]) prune(keep map[string]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if _, ok := keep[key]; !ok {
			delete(c.entries, key)
		}
	}
}

func (c *fileDiscoveryCache[T]) pruneMissing() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if _, err := os.Stat(key); err != nil {
			delete(c.entries, key)
		}
	}
}

func (c *fileDiscoveryCache[T]) pruneBefore(cutoff time.Time) {
	cutoffNS := cutoff.UnixNano()
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if entry.fingerprint.modifiedUnixNS < cutoffNS {
			delete(c.entries, key)
		}
	}
}

func (c *fileDiscoveryCache[T]) stats() (hits, misses uint64, entries int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses, len(c.entries)
}

type jsonlAttentionProbe struct {
	head  [][]byte
	tail  [][]byte
	whole bool
}

// readJSONLAttentionProbe reads at most two small windows and never retains
// their bytes in adapter caches. For small files it returns the complete JSONL;
// for large files it returns only complete records from the head and tail.
func readJSONLAttentionProbe(path string, info os.FileInfo) (jsonlAttentionProbe, error) {
	file, err := os.Open(path)
	if err != nil {
		return jsonlAttentionProbe{}, err
	}
	defer file.Close()

	size := info.Size()
	if size <= 2*attentionProbeBytes {
		raw, readErr := io.ReadAll(io.LimitReader(file, 2*attentionProbeBytes+1))
		if readErr != nil {
			return jsonlAttentionProbe{}, readErr
		}
		return jsonlAttentionProbe{head: splitProbeLines(raw, false, false), whole: true}, nil
	}

	headRaw, err := readProbeWindow(file, size, false)
	if err != nil {
		return jsonlAttentionProbe{}, err
	}
	tailRaw, err := readProbeWindow(file, size, true)
	if err != nil {
		return jsonlAttentionProbe{}, err
	}
	return jsonlAttentionProbe{
		head: splitProbeLines(headRaw, false, true),
		tail: splitProbeLines(tailRaw, true, false),
	}, nil
}

func readProbeWindow(file *os.File, size int64, fromTail bool) ([]byte, error) {
	window := attentionProbeBytes
	for {
		if window > size {
			window = size
		}
		offset := int64(0)
		if fromTail {
			offset = size - window
		}
		raw := make([]byte, window)
		if _, err := file.ReadAt(raw, offset); err != nil && err != io.EOF {
			return nil, err
		}
		if bytes.ContainsRune(raw, '\n') || window == size || window >= attentionProbeRecordLimit {
			return raw, nil
		}
		window = attentionProbeRecordLimit
	}
}

func splitProbeLines(raw []byte, dropFirst, dropLast bool) [][]byte {
	if dropFirst {
		newline := bytes.IndexByte(raw, '\n')
		if newline < 0 {
			return nil
		}
		raw = raw[newline+1:]
	}
	if dropLast && len(raw) > 0 && raw[len(raw)-1] != '\n' {
		newline := bytes.LastIndexByte(raw, '\n')
		if newline < 0 {
			return nil
		}
		raw = raw[:newline+1]
	}
	parts := bytes.Split(raw, []byte{'\n'})
	lines := make([][]byte, 0, len(parts))
	for _, part := range parts {
		part = bytes.TrimSuffix(part, []byte{'\r'})
		if len(part) > 0 {
			lines = append(lines, part)
		}
	}
	return lines
}
