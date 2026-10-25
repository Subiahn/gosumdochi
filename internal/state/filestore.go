package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"gosumdochi/internal/model"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// FileStore keeps one run under dir: events.jsonl (append only),
// evidence/<id>.json (write once) and state.json (atomic replace).
type FileStore struct {
	mu     sync.Mutex
	dir    string
	runID  string
	events *os.File
	seq    int
}

func NewFileStore(dir, runID string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, "evidence"), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	return &FileStore{dir: dir, runID: runID, events: f}, nil
}

func (s *FileStore) Close() error { return s.events.Close() }

type eventLine struct {
	model.Meta
	Seq     int    `json:"seq"`
	Kind    string `json:"kind"`
	Payload any    `json:"payload"`
}

func (s *FileStore) AppendEvent(_ context.Context, kind string, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	line, err := json.Marshal(eventLine{
		Meta: model.Meta{SchemaVersion: model.SchemaVersion, RunID: s.runID, Timestamp: time.Now().UTC()},
		Seq:  s.seq, Kind: kind, Payload: payload,
	})
	if err != nil {
		return err
	}
	if _, err := s.events.Write(append(line, '\n')); err != nil {
		return err
	}
	return s.events.Sync()
}

// PutEvidence refuses to overwrite: evidence is immutable once stored.
func (s *FileStore) PutEvidence(_ context.Context, e model.Evidence) error {
	if !safeID.MatchString(e.ID) {
		return fmt.Errorf("evidence id %q not allowed", e.ID)
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, "evidence", e.ID+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (s *FileStore) GetEvidence(_ context.Context, id string) (model.Evidence, error) {
	var e model.Evidence
	if !safeID.MatchString(id) {
		return e, fmt.Errorf("evidence id %q not allowed", id)
	}
	b, err := os.ReadFile(filepath.Join(s.dir, "evidence", id+".json"))
	if err != nil {
		return e, err
	}
	return model.DecodeStrict[model.Evidence](b)
}

func (s *FileStore) SaveState(_ context.Context, snap Snapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp := filepath.Join(s.dir, "state.json.tmp")
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, "state.json"))
}

func (s *FileStore) LoadState(_ context.Context) (Snapshot, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if err != nil {
		return Snapshot{}, err
	}
	return model.DecodeStrict[Snapshot](b)
}

var _ Store = (*FileStore)(nil)
