package job

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/liuzhaojun/print-portal/internal/storage"
)

// RetentionStats summarizes one cleanup pass.
type RetentionStats struct {
	Jobs    int
	Uploads int
	JobDirs int
	Bytes   int64
}

// PurgeFinished removes all terminal job history together with its files.
// It backs the "clear print history" button: deleting only the database rows
// used to leave orphaned job directories behind forever.
func (m *Manager) PurgeFinished(ctx context.Context) (int, error) {
	finished, err := m.store.ListFinishedJobs()
	if err != nil {
		return 0, err
	}
	all, err := m.store.ListRetentionJobs()
	if err != nil {
		return 0, err
	}
	purge := make(map[int64]bool, len(finished))
	for _, j := range finished {
		purge[j.ID] = true
	}
	// A stored file may be shared by retry clones created by older versions.
	// Only delete it when no surviving job still points at it.
	remaining := remainingStoredRefs(all, purge)

	ids := make([]int64, 0, len(finished))
	for _, j := range finished {
		_ = os.RemoveAll(filepath.Join(m.dataDir, "jobs", strconv.FormatInt(j.ID, 10)))
		if j.StoredPath != "" && remaining[filepath.Clean(j.StoredPath)] == 0 {
			_ = os.Remove(j.StoredPath)
		}
		ids = append(ids, j.ID)
	}
	if err := m.store.DeleteJobRecords(ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// CleanupRetention performs one retention pass:
//
//   - terminal jobs older than job_history_days are removed with their files
//   - job directories without a database row are removed
//   - upload files not referenced by any job are removed after
//     temp_retention_hours
//
// A value of 0 for either setting means "keep forever".
func (m *Manager) CleanupRetention(ctx context.Context) (RetentionStats, error) {
	var stats RetentionStats
	historyDays := m.store.SettingInt("job_history_days", 30)
	tempHours := m.store.SettingInt("temp_retention_hours", 24)

	all, err := m.store.ListRetentionJobs()
	if err != nil {
		return stats, err
	}
	purged := map[int64]bool{}

	// 1. Expired terminal job records.
	if historyDays > 0 {
		cutoff := time.Now().Add(-time.Duration(historyDays) * 24 * time.Hour).Format(time.RFC3339)
		expired, err := m.store.ListExpiredJobs(cutoff)
		if err != nil {
			return stats, err
		}
		for _, j := range expired {
			purged[j.ID] = true
		}
		remaining := remainingStoredRefs(all, purged)
		ids := make([]int64, 0, len(expired))
		for _, j := range expired {
			dir := filepath.Join(m.dataDir, "jobs", strconv.FormatInt(j.ID, 10))
			stats.Bytes += dirSize(dir)
			_ = os.RemoveAll(dir)
			if j.StoredPath != "" && remaining[filepath.Clean(j.StoredPath)] == 0 {
				stats.Bytes += fileSize(j.StoredPath)
				_ = os.Remove(j.StoredPath)
			}
			ids = append(ids, j.ID)
		}
		if len(ids) > 0 {
			if err := m.store.DeleteJobRecords(ids); err != nil {
				return stats, err
			}
			stats.Jobs = len(ids)
		}
	}

	// 2. Job directories without a database row.
	known := make(map[string]bool, len(all))
	referenced := make(map[string]bool, len(all))
	for _, j := range all {
		if purged[j.ID] {
			continue
		}
		known[strconv.FormatInt(j.ID, 10)] = true
		if j.StoredPath != "" {
			referenced[filepath.Clean(j.StoredPath)] = true
		}
	}
	if entries, err := os.ReadDir(filepath.Join(m.dataDir, "jobs")); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || known[entry.Name()] {
				continue
			}
			dir := filepath.Join(m.dataDir, "jobs", entry.Name())
			stats.Bytes += dirSize(dir)
			if err := os.RemoveAll(dir); err == nil {
				stats.JobDirs++
			}
		}
	}

	// 3. Orphan upload files. Referenced files stay until their job record is
	// removed; unreferenced ones are leftovers from failed submissions.
	if tempHours > 0 {
		cutoff := time.Now().Add(-time.Duration(tempHours) * time.Hour)
		if entries, err := os.ReadDir(filepath.Join(m.dataDir, "uploads")); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				full := filepath.Join(m.dataDir, "uploads", entry.Name())
				if referenced[filepath.Clean(full)] {
					continue
				}
				info, err := entry.Info()
				if err != nil || info.ModTime().After(cutoff) {
					continue
				}
				stats.Bytes += info.Size()
				if err := os.Remove(full); err == nil {
					stats.Uploads++
				}
			}
		}
	}

	if stats.Jobs+stats.Uploads+stats.JobDirs > 0 {
		m.logger.Printf("retention cleanup: jobs=%d uploads=%d job_dirs=%d freed=%s",
			stats.Jobs, stats.Uploads, stats.JobDirs, humanBytes(stats.Bytes))
	}
	return stats, nil
}

func remainingStoredRefs(all []storage.RetentionJob, purge map[int64]bool) map[string]int {
	refs := make(map[string]int, len(all))
	for _, j := range all {
		if purge[j.ID] || j.StoredPath == "" {
			continue
		}
		refs[filepath.Clean(j.StoredPath)]++
	}
	return refs
}

func fileSize(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}

func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + []string{"KB", "MB", "GB", "TB"}[exp]
}
