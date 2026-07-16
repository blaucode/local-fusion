package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Charter is a standing, human-approved authorization for a class of chore
// runs (ADR-011): a chore-tier intent references a charter id; the engine
// checks it exists and is not expired. Charters are human-authored — drop a
// JSON file into <data>/charters/<id>.json (see docs/tools.md#lf_plan).
type Charter struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	ApprovedBy string    `json:"approved_by"`
	CreatedAt  time.Time `json:"created_at"`
	// Expires is optional; zero means no expiry.
	Expires time.Time `json:"expires,omitzero"`
}

// ErrCharter reports a missing, corrupt, or expired charter.
var ErrCharter = errors.New("charter")

// ReadConstitution returns the project's constitution.md (ADR-012) — persistent
// human-authored principles — or "" when none is present. Placed in the volume
// like charters; the server never invents it. Callers inject it append-only
// and empty-default (parity-safe).
func (s *Store) ReadConstitution(projectID string) string {
	if err := validateID("project_id", projectID); err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(s.root, "projects", projectID, "constitution.md"))
	if err != nil {
		return ""
	}
	return string(data)
}

// CheckCharter validates a chore-tier intent ref (ADR-011 rule 4).
func (s *Store) CheckCharter(id string) (Charter, error) {
	if err := validateID("charter id", id); err != nil {
		return Charter{}, fmt.Errorf("%w: %v", ErrCharter, err)
	}
	data, err := os.ReadFile(filepath.Join(s.root, "charters", id+".json"))
	if err != nil {
		return Charter{}, fmt.Errorf("%w %q not found — create charters/%s.json in the data volume (docs/tools.md#lf_plan)", ErrCharter, id, id)
	}
	var c Charter
	if err := json.Unmarshal(data, &c); err != nil {
		return Charter{}, fmt.Errorf("%w %q is corrupt: %v", ErrCharter, id, err)
	}
	if c.ApprovedBy == "" {
		return Charter{}, fmt.Errorf("%w %q has no approved_by — charters require a human approval mark", ErrCharter, id)
	}
	if !c.Expires.IsZero() && time.Now().After(c.Expires) {
		return Charter{}, fmt.Errorf("%w %q expired %s", ErrCharter, id, c.Expires.Format("2006-01-02"))
	}
	return c, nil
}
