// Package taskruns keeps finished clicky task runs in the UIR database. Store is the clicky RunStore
// every uir process installs, so a run is listed by `uir serve` whichever process ran it, and after a
// restart.
package taskruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	clickytask "github.com/flanksource/clicky/task"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// StoredLogEntries is how many of its newest log entries each stored task keeps.
	StoredLogEntries = 500
	// StoredStreamBytes is how much of the tail of its stdout and stderr each stored task keeps.
	StoredStreamBytes = 64 << 10
)

// Run is one row of task_runs: the listing columns, and the run's RunMeta and bounded snapshots as
// clicky reports them.
type Run struct {
	ID          string       `gorm:"column:id;primaryKey"`
	Name        string       `gorm:"column:name"`
	Kind        string       `gorm:"column:kind"`
	Status      string       `gorm:"column:status"`
	Labels      storage.JSON `gorm:"column:labels"`
	StartedAt   *time.Time   `gorm:"column:started_at"`
	FinishedAt  *time.Time   `gorm:"column:finished_at"`
	Error       string       `gorm:"column:error"`
	SnapshotIDs storage.JSON `gorm:"column:snapshot_ids"`
	Meta        storage.JSON `gorm:"column:meta"`
	Snapshots   storage.JSON `gorm:"column:snapshots"`
	SavedAt     time.Time    `gorm:"column:saved_at"`
}

func (Run) TableName() string { return "task_runs" }

// RunDetails is the part of a run's group details the store reads: the snapshots the run published.
type RunDetails struct {
	SnapshotIDs []string `json:"snapshot_ids"`
}

// Store is a clicky RunStore over task_runs.
type Store struct {
	database *gorm.DB
}

var _ clickytask.RunStore = (*Store)(nil)

func New(database *gorm.DB) *Store {
	if database == nil {
		panic("taskruns: database is required")
	}
	return &Store{database: database}
}

// SaveRun stores the run, replacing an earlier save of it.
func (store *Store) SaveRun(ctx context.Context, groupID string, snapshots []clickytask.TaskSnapshot) error {
	record, found := clickytask.RunFromSnapshots(snapshots)
	if !found {
		return fmt.Errorf("save task run %s: it has no group snapshot", groupID)
	}
	if record.ID != groupID {
		return fmt.Errorf("save task run %s: its group snapshot is run %s", groupID, record.ID)
	}
	row, err := newRun(record.RunMeta, boundSnapshots(snapshots))
	if err != nil {
		return fmt.Errorf("save task run %s: %w", groupID, err)
	}
	if err := store.database.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(&row).Error; err != nil {
		return fmt.Errorf("save task run %s: %w", groupID, err)
	}
	return nil
}

// Runs lists stored runs, newest first, narrowed by filter.
func (store *Store) Runs(ctx context.Context, filter clickytask.RunFilter) ([]clickytask.RunMeta, error) {
	query := store.database.WithContext(ctx).Model(&Run{})
	if filter.Kind != "" {
		query = query.Where("kind = ?", filter.Kind)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var rows []Run
	if err := query.Select("id", "meta").Order("started_at DESC").Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list stored task runs: %w", err)
	}
	runs := make([]clickytask.RunMeta, 0, len(rows))
	for _, row := range rows {
		var meta clickytask.RunMeta
		if err := json.Unmarshal(row.Meta, &meta); err != nil {
			return nil, fmt.Errorf("decode stored task run %s: %w", row.ID, err)
		}
		if filter.Matches(meta) {
			runs = append(runs, meta)
		}
	}
	return runs, nil
}

// Snapshot returns a stored run's group and task snapshots, and none for a run it does not hold.
func (store *Store) Snapshot(ctx context.Context, id string) ([]clickytask.TaskSnapshot, error) {
	var row Run
	err := store.database.WithContext(ctx).Select("id", "snapshots").Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load stored task run %s: %w", id, err)
	}
	var snapshots []clickytask.TaskSnapshot
	if err := json.Unmarshal(row.Snapshots, &snapshots); err != nil {
		return nil, fmt.Errorf("decode stored task run %s: %w", id, err)
	}
	return snapshots, nil
}

// Control refuses: a stored run finished, or belongs to a process that no longer holds it.
func (store *Store) Control(_ context.Context, id string, action clickytask.ControlAction) error {
	return fmt.Errorf("run %s is not running in this process; a stored run cannot %s", id, action)
}

func newRun(meta clickytask.RunMeta, snapshots []clickytask.TaskSnapshot) (Run, error) {
	meta.Controls = nil
	row := Run{ID: meta.ID, Name: meta.Name, Kind: meta.Kind, Status: meta.Status, Error: taskErrors(snapshots), SavedAt: time.Now().UTC()}
	var err error
	if row.StartedAt, err = parseTime(meta.StartedAt); err != nil {
		return Run{}, fmt.Errorf("started at: %w", err)
	}
	if row.FinishedAt, err = parseTime(meta.FinishedAt); err != nil {
		return Run{}, fmt.Errorf("finished at: %w", err)
	}
	details, err := runDetails(snapshots)
	if err != nil {
		return Run{}, err
	}
	labels := meta.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	for column, value := range map[*storage.JSON]any{&row.Labels: labels, &row.SnapshotIDs: details.SnapshotIDs, &row.Meta: meta, &row.Snapshots: snapshots} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return Run{}, err
		}
		*column = encoded
	}
	return row, nil
}

// runDetails reads the snapshot ids from the group snapshot's details; a run without them published
// none.
func runDetails(snapshots []clickytask.TaskSnapshot) (RunDetails, error) {
	details := RunDetails{SnapshotIDs: []string{}}
	for _, snapshot := range snapshots {
		if snapshot.Type != "group" || snapshot.Details == nil {
			continue
		}
		encoded, err := json.Marshal(snapshot.Details)
		if err != nil {
			return RunDetails{}, fmt.Errorf("encode run details: %w", err)
		}
		if err := json.Unmarshal(encoded, &details); err != nil {
			return RunDetails{}, fmt.Errorf("decode run details: %w", err)
		}
		if details.SnapshotIDs == nil {
			details.SnapshotIDs = []string{}
		}
	}
	return details, nil
}

func taskErrors(snapshots []clickytask.TaskSnapshot) string {
	var failures []string
	for _, snapshot := range snapshots {
		if snapshot.Type == "task" && snapshot.Error != "" {
			failures = append(failures, snapshot.Name+": "+snapshot.Error)
		}
	}
	return strings.Join(failures, "; ")
}

func parseTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// boundSnapshots keeps each task's newest StoredLogEntries log entries and the last StoredStreamBytes
// of its output, and drops controls, which a stored run cannot honour.
func boundSnapshots(snapshots []clickytask.TaskSnapshot) []clickytask.TaskSnapshot {
	bounded := make([]clickytask.TaskSnapshot, len(snapshots))
	for index, snapshot := range snapshots {
		snapshot.Controls = nil
		if len(snapshot.Logs) > StoredLogEntries {
			snapshot.Logs = snapshot.Logs[len(snapshot.Logs)-StoredLogEntries:]
		}
		snapshot.Stdout, snapshot.StdoutOffset, snapshot.StdoutTruncated = streamTail(snapshot.Stdout, snapshot.StdoutOffset, snapshot.StdoutTruncated)
		snapshot.Stderr, snapshot.StderrOffset, snapshot.StderrTruncated = streamTail(snapshot.Stderr, snapshot.StderrOffset, snapshot.StderrTruncated)
		bounded[index] = snapshot
	}
	return bounded
}

func streamTail(value string, offset int64, truncated bool) (string, int64, bool) {
	if len(value) <= StoredStreamBytes {
		return value, offset, truncated
	}
	dropped := len(value) - StoredStreamBytes
	return value[dropped:], offset + int64(dropped), true
}
