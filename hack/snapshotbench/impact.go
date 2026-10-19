package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

func runImpact(ctx context.Context, args []string) (returnErr error) {
	options, err := parseImpactOptions(args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.Output), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(options.Output, 0o755); err != nil {
		return err
	}
	run := impactRun{Version: 1, StartedAt: time.Now().UTC(), Options: options, GoVersion: runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH, GOMAXPROCS: runtime.GOMAXPROCS(0)}
	defer func() {
		if returnErr != nil {
			run.Error = returnErr.Error()
		}
		returnErr = errors.Join(returnErr, writeJSON(filepath.Join(options.Output, "results.json"), run))
	}()
	seed := filepath.Join(options.Output, "seed.db")
	if run.CorpusSHA, err = copyImpactDatabase(options.Database, seed); err != nil {
		return err
	}
	if run.ProducerSHA, err = executableDigest(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := copyExecutable(executable, filepath.Join(options.Output, "runner")); err != nil {
		return err
	}
	if err := impactReadQueries(ctx, &run, seed); err != nil {
		return err
	}
	if options.Only != "graph" {
		if err := impactPruning(ctx, &run, seed); err != nil {
			return err
		}
	}
	run.Complete = true
	return nil
}

func parseImpactOptions(args []string) (impactOptions, error) {
	var options impactOptions
	flags := flag.NewFlagSet("impact", flag.ContinueOnError)
	flags.StringVar(&options.Database, "db", "", "quiescent SQLite Captain corpus; the original is never opened")
	flags.StringVar(&options.Output, "out", "", "new absolute output directory under .tmp")
	flags.StringVar(&options.Label, "label", "", "before or after")
	flags.IntVar(&options.Samples, "samples", 10, "samples per cohort, after one unrecorded warmup")
	flags.StringVar(&options.Root, "root", "github.com/flanksource/captain", "indexed root")
	flags.StringVar(&options.From, "from", captainFirst, "earlier indexed Git commit")
	flags.StringVar(&options.To, "to", captainSecond, "later indexed Git commit")
	flags.IntVar(&options.Depth, "depth", 4, "graph depth")
	flags.IntVar(&options.Limit, "limit", 150, "graph node limit")
	flags.StringVar(&options.Only, "only", "all", "all, graph or prune")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if options.Database == "" || !filepath.IsAbs(options.Output) || options.Samples < 1 ||
		(options.Label != "before" && options.Label != "after") ||
		(options.Only != "all" && options.Only != "graph" && options.Only != "prune") {
		return options, errors.New("impact requires --db, a new absolute --out, --label before|after, positive --samples and --only all|graph|prune")
	}
	return options, nil
}

func copyImpactDatabase(source, target string) (digest string, returnErr error) {
	if wal, err := os.Stat(source + "-wal"); err == nil && wal.Size() != 0 {
		return "", fmt.Errorf("%s has a nonempty WAL; use a quiescent checkpointed corpus", source)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	reader, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer func() { returnErr = errors.Join(returnErr, reader.Close()) }()
	before, err := reader.Stat()
	if err != nil {
		return "", err
	}
	writer, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer func() { returnErr = errors.Join(returnErr, writer.Close()) }()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(writer, hash), reader); err != nil {
		return "", err
	}
	after, err := reader.Stat()
	if err != nil {
		return "", err
	}
	if before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return "", fmt.Errorf("corpus %s changed while copying", source)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func impactReadQueries(ctx context.Context, run *impactRun, seed string) (returnErr error) {
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: seed})
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeDB(database)) }()
	if run.From, err = impactSnapshot(ctx, database, run.Options.Root, run.Options.From); err != nil {
		return err
	}
	if run.To, err = impactSnapshot(ctx, database, run.Options.Root, run.Options.To); err != nil {
		return err
	}
	if run.From.ConfigurationHash != run.To.ConfigurationHash || run.To.BaseSnapshotID == nil || *run.To.BaseSnapshotID != run.From.ID {
		return errors.New("impact corpus must hold comparable snapshots with the later snapshot based on the earlier")
	}
	if run.Options.Only == "prune" {
		return nil
	}
	return impactGraphs(ctx, run, database)
}

func impactSnapshot(ctx context.Context, database *gorm.DB, root, commit string) (storage.ModuleSnapshot, error) {
	var snapshot storage.ModuleSnapshot
	err := database.WithContext(ctx).Table("snapshots AS s").Select("s.*").Joins("JOIN modules m ON m.id = s.root_id").
		Where("m.root_key = ? AND s.git_commit = ?", root, commit).Order("s.ordinal DESC").Take(&snapshot).Error
	return snapshot, err
}

func timeImpact(operation func() ([]byte, error)) (impactObservation, error) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	body, err := operation()
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	return impactObservation{ElapsedNS: elapsed.Nanoseconds(), AllocatedBytes: after.TotalAlloc - before.TotalAlloc, ResponseBytes: len(body)}, err
}

func recordImpact(run *impactRun, observed impactObservation) error {
	run.Observations = append(run.Observations, observed)
	fmt.Fprintf(os.Stderr, "%s %s/%s %02d: %.3f ms, %.2f MiB allocated\n", run.Options.Label,
		observed.Case, observed.Mode, observed.Iteration, float64(observed.ElapsedNS)/1e6, float64(observed.AllocatedBytes)/(1<<20))
	return writeJSON(filepath.Join(run.Options.Output, "results.json"), run)
}
