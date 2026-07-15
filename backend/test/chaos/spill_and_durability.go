package main

// Two targeted edge cases named explicitly in the phase brief:
//   - spill-file replay after a partial/torn write (the checksum path)
//   - the Redis AOF `appendfsync everysec` durability window, measured
//     empirically rather than assumed (sub-PRD 03 §7 open question).

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// scenarioSpillCorruption forces the ingest onto its disk-WAL fallback
// (Redis down), hand-splices one deliberately corrupt line into the WAL —
// the shape spill.go's CRC32 guard exists for — then proves the drain skips
// only that line and still recovers every good record intact.
func (r *Rig) scenarioSpillCorruption(ctx context.Context) (*ScenarioResult, error) {
	if err := r.ensureAcctRunning(); err != nil {
		return nil, err
	}
	nasID, nasIP, err := r.provisionNAS(ctx, "pppoe")
	if err != nil {
		return nil, err
	}
	before, err := fetchCounters(r.AcctAddr)
	if err != nil {
		return nil, err
	}

	if err := dockerKill(r.RedisContainer); err != nil {
		return nil, err
	}

	sessions := newSessions(5, nasIP, 1)
	var goodRecords int64
	for _, s := range sessions {
		for _, step := range s.steps {
			rec := buildRecord(s, step)
			ok, _, _ := postRecord(r.AcctAddr, rec, 5*time.Second)
			if !ok {
				return &ScenarioResult{Pass: false, Detail: "record not acked while spilling (Redis down)"}, nil
			}
			goodRecords++
		}
	}

	// Stop acct so nothing else is writing the WAL, then splice in the
	// torn/corrupt line by hand.
	if err := r.killAcct(); err != nil {
		return nil, err
	}
	if err := appendCorruptSpillLine(filepath.Join(r.spillDir, "acct-spill.wal")); err != nil {
		return nil, err
	}

	if err := dockerStart(r.RedisContainer); err != nil {
		return nil, err
	}
	if err := waitTCP(redisHostPort(r.RedisURL), 30*time.Second); err != nil {
		return nil, err
	}
	if err := r.startAcct(); err != nil {
		return nil, err
	}

	after, drained := waitInvariant(r.AcctAddr, 30*time.Second)
	pass := drained && after.Persisted-before.Persisted == goodRecords
	detail := fmt.Sprintf("good_records=%d persisted_delta=%d (1 hand-corrupted WAL line spliced in; drain must skip it, not crash, and lose nothing else)",
		goodRecords, after.Persisted-before.Persisted)
	return &ScenarioResult{Pass: pass, Detail: detail, Counters: after, Extra: map[string]any{"nas_id": nasID}}, nil
}

// appendCorruptSpillLine writes one well-formed-shaped ("<crc>\t<base64>\n")
// but CRC-mismatched line — spill.go's decodeSpillLine rejects it exactly
// like it would a line torn by an unclean shutdown mid-write.
func appendCorruptSpillLine(path string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line := "999999999\t" + base64.StdEncoding.EncodeToString([]byte(`{"record_type":"stop","acct_session_id":"torn"}`)) + "\n"
	_, err = f.WriteString(line)
	return err
}

// scenarioRedisDurability empirically measures the Redis AOF
// `appendfsync everysec` durability window: sends a burst, hard-kills Redis
// (SIGKILL, no graceful BGSAVE) within that window, restarts it, and reports
// how many acked records the stream actually lost. This is a measurement,
// not a pass/fail gate — the DoD requires the open question closed WITH
// data, which is what Extra.measured_loss is for (see
// docs/evidence/redis-durability-decision.md for the resulting decision).
func (r *Rig) scenarioRedisDurability(ctx context.Context) (*ScenarioResult, error) {
	if err := r.ensureAcctRunning(); err != nil {
		return nil, err
	}
	nasID, nasIP, err := r.provisionNAS(ctx, "pppoe")
	if err != nil {
		return nil, err
	}
	before, err := fetchCounters(r.AcctAddr)
	if err != nil {
		return nil, err
	}

	sessions := newSessions(30, nasIP, 0) // start+stop only: a fast burst
	var sent int64
	for _, s := range sessions {
		for _, step := range s.steps {
			rec := buildRecord(s, step)
			ok, _, _ := postRecord(r.AcctAddr, rec, 5*time.Second)
			if ok {
				sent++
			}
		}
	}

	// Hard-kill within the AOF everysec window (well under 1s since the last
	// ack) — no BGSAVE, no graceful stop. Same container, so the writable
	// layer (and whatever was fsynced to the AOF) survives the kill.
	time.Sleep(200 * time.Millisecond)
	if err := dockerKill(r.RedisContainer); err != nil {
		return nil, err
	}
	time.Sleep(1 * time.Second)
	if err := dockerStart(r.RedisContainer); err != nil {
		return nil, err
	}
	if err := waitTCP(redisHostPort(r.RedisURL), 30*time.Second); err != nil {
		return nil, err
	}
	time.Sleep(3 * time.Second) // let hikrad-acct's client reconnect and the consumer resume

	after, _ := waitInvariant(r.AcctAddr, 20*time.Second)
	lost := sent - (after.Persisted - before.Persisted)
	detail := fmt.Sprintf("sent=%d persisted_delta=%d measured_loss=%d (AOF everysec hard-kill within 200ms of last ack)",
		sent, after.Persisted-before.Persisted, lost)
	return &ScenarioResult{
		Pass:     true,
		Detail:   detail,
		Counters: after,
		Extra:    map[string]any{"nas_id": nasID, "sent": sent, "measured_loss": lost},
	}, nil
}
