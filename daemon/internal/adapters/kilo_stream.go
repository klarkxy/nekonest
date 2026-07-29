package adapters

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// StreamEmitter pushes progressive chat parts (stable id for patching).
type StreamEmitter func(sessionID, partID, msgType, content string)

// StartDBStream polls kilo.db while a session is generating so the phone
// gets near-live text (CLI --format json only emits finalized parts).
// Call stop() when the process exits.
func (a *KiloAdapter) StartDBStream(sessionID string, emit StreamEmitter) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	stop = func() {
		once.Do(func() { close(done) })
		<-finished
	}

	go func() {
		defer close(finished)
		// Snapshot existing parts so we only stream new/growing ones after prompt.
		seen := a.snapshotPartLens(sessionID)
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		idleRounds := 0
		for {
			select {
			case <-done:
				// Final flush
				a.pollPartsOnce(sessionID, seen, emit)
				return
			case <-ticker.C:
				grew := a.pollPartsOnce(sessionID, seen, emit)
				if grew {
					idleRounds = 0
				} else {
					idleRounds++
					// Keep polling a bit after silence (model may pause mid-turn)
					if idleRounds > 150 { // ~60s quiet → stop self if caller forgot
						return
					}
				}
			}
		}
	}()
	return stop
}

func (a *KiloAdapter) openRO() (*sql.DB, error) {
	if _, err := os.Stat(a.dbPath); os.IsNotExist(err) {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(500)", filepath.ToSlash(a.dbPath))
	return sql.Open("sqlite", dsn)
}

func (a *KiloAdapter) snapshotPartLens(sessionID string) map[string]int {
	out := make(map[string]int)
	db, err := a.openRO()
	if err != nil {
		return out
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT p.id, COALESCE(json_extract(p.data, '$.text'), '')
		FROM part p
		WHERE p.session_id = ?
		  AND json_extract(p.data, '$.type') = 'text'
	`, sessionID)
	if err != nil {
		return out
	}
	for rows.Next() {
		var id, text string
		if rows.Scan(&id, &text) == nil {
			out[id] = len(text)
		}
	}
	rows.Close()
	// Mark existing tool parts so we do not re-emit every poll.
	trows, err := db.Query(`
		SELECT p.id FROM part p
		WHERE p.session_id = ?
		  AND json_extract(p.data, '$.type') = 'tool'
	`, sessionID)
	if err != nil {
		return out
	}
	defer trows.Close()
	for trows.Next() {
		var id string
		if trows.Scan(&id) == nil {
			out["tool:"+id] = 1
		}
	}
	return out
}

// pollPartsOnce emits new/grown text (and new tool) parts. Returns true if anything emitted.
func (a *KiloAdapter) pollPartsOnce(sessionID string, seen map[string]int, emit StreamEmitter) bool {
	if emit == nil {
		return false
	}
	db, err := a.openRO()
	if err != nil {
		return false
	}
	defer db.Close()

	grew := false

	// Text parts — progressive content by part id
	rows, err := db.Query(`
		SELECT
			p.id,
			COALESCE(json_extract(m.data, '$.role'), 'assistant'),
			COALESCE(json_extract(p.data, '$.text'), ''),
			COALESCE(json_extract(p.data, '$.ignored'), 0)
		FROM part p
		JOIN message m ON m.id = p.message_id
		WHERE p.session_id = ?
		  AND json_extract(p.data, '$.type') = 'text'
		  AND json_extract(m.data, '$.role') IN ('user', 'assistant')
		ORDER BY p.time_created ASC
	`, sessionID)
	if err != nil {
		log.Printf("[kilo-stream] query text: %v", err)
		return false
	}
	for rows.Next() {
		var id, role, text string
		var ignored int
		if err := rows.Scan(&id, &role, &text, &ignored); err != nil {
			continue
		}
		if ignored != 0 {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		prev, ok := seen[id]
		if ok && len(text) <= prev {
			continue
		}
		// Skip pure user echoes that already existed at snapshot (ok && ...)
		// New user messages after start still stream (not in seen).
		// Never stream user turns — phone already shows optimistic bubble;
		// re-emitting causes "duplicate send" appearance and can confuse history merge.
		if role == "user" {
			seen[id] = len(text)
			continue
		}
		seen[id] = len(text)
		emit(sessionID, id, "assistant", truncateRunes(text, 16000))
		grew = true
	}
	rows.Close()

	// Tool parts (name only, once)
	trows, err := db.Query(`
		SELECT
			p.id,
			COALESCE(json_extract(p.data, '$.tool'), json_extract(p.data, '$.name'), 'tool'),
			COALESCE(json_extract(p.data, '$.state.status'), '')
		FROM part p
		WHERE p.session_id = ?
		  AND json_extract(p.data, '$.type') = 'tool'
		ORDER BY p.time_created ASC
	`, sessionID)
	if err == nil {
		for trows.Next() {
			var id, tool, status string
			if trows.Scan(&id, &tool, &status) != nil {
				continue
			}
			key := "tool:" + id
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = 1
			label := tool
			if status != "" && status != "completed" {
				label = fmt.Sprintf("%s (%s)", tool, status)
			}
			emit(sessionID, id, "tool_call", fmt.Sprintf("🔧 %s", label))
			grew = true
		}
		trows.Close()
	}

	return grew
}
