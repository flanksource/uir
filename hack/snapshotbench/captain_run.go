package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	captainFirst  = "567b8ab33aec1c8aafcefec233894d16153e4337"
	captainSecond = "246a1cbfc2e48dc5567165a68890c734cc02e99d"
)

type captainRun struct {
	Directory    string              `json:"directory"`
	StartedAt    time.Time           `json:"started_at"`
	First        string              `json:"first"`
	Second       string              `json:"second"`
	Profiled     bool                `json:"profiled"`
	ProducerSHA  string              `json:"producer_sha256"`
	ProducerGit  string              `json:"producer_git_commit"`
	ProducerTree string              `json:"producer_git_status"`
	GoVersion    string              `json:"go_version"`
	GoEnv        string              `json:"go_env"`
	Sources      map[string]string   `json:"sources"`
	Checkouts    map[string]string   `json:"checkouts"`
	Cases        []captainCaseResult `json:"cases"`
}

func runCaptain(ctx context.Context, args []string) (returnErr error) {
	flags := flag.NewFlagSet("captain", flag.ContinueOnError)
	output := flags.String("out", "", "new directory beneath .tmp/captain-profile")
	captain := flags.String("captain", "/Users/moshe/go/src/github.com/flanksource/captain", "Captain source checkout")
	clicky := flags.String("clicky", "/Users/moshe/go/src/github.com/flanksource/clicky", "Clicky source checkout")
	commonsDB := flags.String("commons-db", "/Users/moshe/go/src/github.com/flanksource/commons-db", "commons-db source checkout")
	first := flags.String("first", captainFirst, "first historical commit")
	second := flags.String("second", captainSecond, "second historical commit")
	profile := flags.Bool("profile", true, "capture CPU, heap, trace and SQL profiles")
	if err := flags.Parse(args); err != nil {
		return err
	}
	working, err := os.Getwd()
	if err != nil {
		return err
	}
	directory, err := captainOutput(working, *output)
	if err != nil {
		return err
	}
	sources := map[string]string{"captain": *captain, "clicky": *clicky, "commons-db": *commonsDB}
	for name, source := range sources {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return fmt.Errorf("%s source: %w", name, err)
		}
		sources[name] = absolute
	}
	for _, commit := range []string{*first, *second} {
		resolved, err := commandOutput(ctx, "git", "-C", sources["captain"], "rev-parse", "--verify", commit+"^{commit}")
		if err != nil {
			return err
		}
		if commit != resolved {
			return fmt.Errorf("captain commit %q resolves to %q; supply the full SHA", commit, resolved)
		}
	}
	if *first == *second {
		return errors.New("captain commits must differ")
	}
	if _, err := commandOutput(ctx, "git", "-C", sources["captain"], "merge-base", "--is-ancestor", *first, *second); err != nil {
		return fmt.Errorf("first Captain commit must precede second: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(directory), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(directory, 0o755); err != nil {
		return err
	}
	run := captainRun{Directory: directory, StartedAt: time.Now().UTC(), First: *first, Second: *second, Profiled: *profile, Sources: sources, Checkouts: map[string]string{}}
	defer func() { returnErr = errors.Join(returnErr, writeJSON(filepath.Join(directory, "manifest.json"), run)) }()
	if run.ProducerSHA, err = executableDigest(); err != nil {
		return err
	}
	if run.ProducerGit, err = commandOutput(ctx, "git", "-C", working, "rev-parse", "HEAD"); err != nil {
		return err
	}
	if run.ProducerTree, err = commandOutput(ctx, "git", "-C", working, "status", "--porcelain=v1"); err != nil {
		return err
	}
	if run.GoVersion, err = commandOutput(ctx, "go", "version"); err != nil {
		return err
	}
	if run.GoEnv, err = commandOutput(ctx, "go", "env", "GOFLAGS", "GOWORK", "GOCACHE", "GOMODCACHE"); err != nil {
		return err
	}
	for _, name := range []string{"captain", "clicky", "commons-db"} {
		destination := filepath.Join(directory, "checkouts", name)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if _, err := commandOutput(ctx, "git", "clone", "--no-hardlinks", sources[name], destination); err != nil {
			return err
		}
		if run.Checkouts[name], err = filepath.EvalSymlinks(destination); err != nil {
			return err
		}
	}
	databasePath := filepath.Join(directory, "uir.db")
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: databasePath})
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeDB(database)) }()
	if err := registerCaptainCheckouts(ctx, database, run.Checkouts); err != nil {
		return err
	}
	for _, entry := range captainMeasurements(*first, *second) {
		entry.Checkout, entry.Database, entry.Directory = run.Checkouts["captain"], databasePath, filepath.Join(directory, entry.ID)
		if err := os.Mkdir(entry.Directory, 0o755); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", entry.ID, entry.Commit)
		observed, err := measureCaptainCase(ctx, database, entry, *profile)
		run.Cases = append(run.Cases, observed)
		if writeErr := writeJSON(filepath.Join(entry.Directory, "result.json"), observed); writeErr != nil {
			return errors.Join(err, writeErr)
		}
		if err != nil {
			return err
		}
	}
	if err := reportCaptain(ctx, database, run); err != nil {
		return err
	}
	return nil
}

func captainOutput(working, output string) (string, error) {
	base := filepath.Join(working, ".tmp", "captain-profile")
	if output == "" {
		output = filepath.Join(base, time.Now().Format("20060102-150405"))
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(base, absolute)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return "", errors.New("captain profile output must be a new directory beneath .tmp/captain-profile")
	}
	return absolute, nil
}

func executableDigest() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func registerCaptainCheckouts(ctx context.Context, database *gorm.DB, checkouts map[string]string) error {
	var clickyLocation uuid.UUID
	for _, item := range []struct{ key, path, name, kind, mount string }{
		{"github.com/flanksource/clicky", checkouts["clicky"], "clicky", "git", ""},
		{"github.com/flanksource/clicky/aichat", filepath.Join(checkouts["clicky"], "aichat"), "aichat", "module", "aichat"},
		{"github.com/flanksource/commons-db", checkouts["commons-db"], "commons-db", "git", ""},
		{"github.com/flanksource/captain", checkouts["captain"], "captain", "git", ""},
	} {
		path, err := filepath.EvalSymlinks(item.path)
		if err != nil {
			return fmt.Errorf("register %s: %w", item.key, err)
		}
		root := storage.ModuleRoot{ID: uuid.New(), RootKey: item.key, Name: item.name, CreatedAt: time.Now().UTC()}
		if err := storage.CreateModuleRoot(ctx, database, &root); err != nil {
			return err
		}
		location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: path, Kind: item.kind, MountPath: item.mount, CreatedAt: time.Now().UTC()}
		if item.mount != "" {
			location.ParentLocationID = &clickyLocation
		}
		if err := database.WithContext(ctx).Create(&location).Error; err != nil {
			return fmt.Errorf("register %s location: %w", item.key, err)
		}
		if err := database.WithContext(ctx).Create(&storage.ModulePrimary{RootID: root.ID, LocationID: location.ID}).Error; err != nil {
			return fmt.Errorf("set %s primary location: %w", item.key, err)
		}
		if strings.HasSuffix(item.key, "/clicky") {
			clickyLocation = location.ID
		}
	}
	return nil
}
