package accounting

// Disk spill WAL (FR-37.1/37.3, NFR-2). When Redis is unavailable the ingest
// path cannot XADD, so it appends the record to a local fsync'd write-ahead log
// instead and still acks the NAS — the packet is durable on disk. On recovery
// the WAL is drained back into the stream in order and truncated. Each line
// carries a CRC32 so a torn tail from an unclean shutdown is detected and
// skipped, never crashing the drain (edge case in the brief).
//
// Replaying a line into the stream is idempotent end to end: the consumer dedups
// by the DB key, so a drain interrupted after some XADDs and restarted re-pushes
// at worst a few already-seen records, which are counted as duplicates and
// dropped. That lets the drain be a dead-simple "push everything, then truncate".

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type spillFile struct {
	dir     string
	path    string
	mu      sync.Mutex
	f       *os.File
	pendN   atomic.Int64
	enabled bool
}

// newSpill opens (creating as needed) the WAL under dir. An empty dir disables
// spill (unit tests that never exercise the Redis-down path).
func newSpill(dir string) (*spillFile, error) {
	s := &spillFile{dir: dir}
	if dir == "" {
		return s, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("accounting: create spill dir: %w", err)
	}
	s.path = filepath.Join(dir, "acct-spill.wal")
	// O_RDWR (not O_APPEND): Windows refuses Truncate on an append-only handle,
	// so appends seek to end explicitly under the mutex and truncate keeps write
	// access for the drain reset.
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("accounting: open spill wal: %w", err)
	}
	s.f = f
	s.enabled = true
	// Count any records left by a previous crash so in_queue reflects them until
	// the first successful drain.
	s.pendN.Store(int64(s.countLines()))
	return s, nil
}

func (s *spillFile) pending() int64 {
	if s == nil {
		return 0
	}
	return s.pendN.Load()
}

// append fsync-durably writes one record line. Returns an error only when the
// WAL itself cannot be persisted — the caller then fails the ack so the NAS
// retransmits (nothing is lost).
func (s *spillFile) append(payload []byte) error {
	if !s.enabled {
		return errors.New("accounting: spill disabled (no spill dir configured)")
	}
	line := encodeSpillLine(payload)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	if _, err := s.f.WriteString(line); err != nil {
		return err
	}
	if err := s.f.Sync(); err != nil {
		return err
	}
	s.pendN.Add(1)
	return nil
}

// drain pushes every intact record back through push (typically an XADD) and,
// on full success, truncates the WAL. Corrupt lines are skipped and reported via
// badLines. A push error stops the drain and leaves the WAL intact for a retry.
func (s *spillFile) drain(push func(payload []byte) error) (drained, badLines int, err error) {
	if !s.enabled {
		return 0, 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, rerr := os.ReadFile(s.path)
	if rerr != nil {
		if errors.Is(rerr, os.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, rerr
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		payload, ok := decodeSpillLine(line)
		if !ok {
			badLines++
			continue
		}
		if perr := push(payload); perr != nil {
			// Leave the WAL as-is; a later drain retries from the top. Already-
			// pushed records dedup downstream.
			return drained, badLines, perr
		}
		drained++
	}
	if serr := sc.Err(); serr != nil {
		return drained, badLines, serr
	}
	// Everything intact has been re-queued: reset the WAL.
	if err := s.truncate(); err != nil {
		return drained, badLines, err
	}
	s.pendN.Store(0)
	return drained, badLines, nil
}

func (s *spillFile) truncate() error {
	if err := s.f.Truncate(0); err != nil {
		return err
	}
	if _, err := s.f.Seek(0, 0); err != nil {
		return err
	}
	return s.f.Sync()
}

func (s *spillFile) close() error {
	if s.f == nil {
		return nil
	}
	return s.f.Close()
}

func (s *spillFile) countLines() int {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// Line format: "<crc32>\t<base64(payload)>\n". base64 keeps the payload on a
// single line regardless of content; the CRC guards against a torn write.
func encodeSpillLine(payload []byte) string {
	sum := crc32.ChecksumIEEE(payload)
	return strconv.FormatUint(uint64(sum), 10) + "\t" + base64.StdEncoding.EncodeToString(payload) + "\n"
}

func decodeSpillLine(line string) ([]byte, bool) {
	tab := strings.IndexByte(line, '\t')
	if tab < 0 {
		return nil, false
	}
	sum, err := strconv.ParseUint(strings.TrimSpace(line[:tab]), 10, 32)
	if err != nil {
		return nil, false
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line[tab+1:]))
	if err != nil {
		return nil, false
	}
	if crc32.ChecksumIEEE(payload) != uint32(sum) {
		return nil, false
	}
	return payload, true
}
