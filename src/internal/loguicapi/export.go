package loguicapi

import (
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/underundre/unet/internal/logstream"
)

// ExportFormat defines the output format for log exports.
type ExportFormat string

const (
	ExportJSONL ExportFormat = "jsonl"
	ExportGZIP  ExportFormat = "gz"
	ExportZIP   ExportFormat = "zip"
	ExportCSV   ExportFormat = "csv"
)

// ExportOptions configures a log export operation.
type ExportOptions struct {
	Format    ExportFormat
	StartTime time.Time
	EndTime   time.Time
	Level     string // filter by level (empty = all)
	Component string // filter by component (empty = all)
	Source    string // filter by source (empty = all)
	Limit     int    // max records (0 = unlimited)
}

// ExportResult contains the export output path and stats.
type ExportResult struct {
	Path     string
	Records  int
	Size     int64
	Duration time.Duration
}

// Exporter exports log records from the ring buffer or files.
type Exporter struct {
	ring   *logstream.Ring
	logDir string
}

// NewExporter creates a new log exporter.
func NewExporter(ring *logstream.Ring, logDir string) *Exporter {
	return &Exporter{ring: ring, logDir: logDir}
}

// ExportRing exports records from the ring buffer to a file.
func (e *Exporter) ExportRing(opts ExportOptions) (*ExportResult, error) {
	start := time.Now()
	records := e.ring.Snapshot()

	// Apply filters
	filtered := filterRecords(records, opts)
	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}

	// Write to file
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("unet-export-*.%s", opts.Format))
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer tmpFile.Close()

	var writer io.Writer = tmpFile

	// Wrap for compression
	var gzWriter *gzip.Writer
	var zipWriter *zip.Writer
	switch opts.Format {
	case ExportGZIP:
		gzWriter = gzip.NewWriter(tmpFile)
		defer gzWriter.Close()
		writer = gzWriter
	case ExportZIP:
		zipWriter = zip.NewWriter(tmpFile)
		defer zipWriter.Close()
		w, err := zipWriter.Create("export.jsonl")
		if err != nil {
			return nil, fmt.Errorf("create zip entry: %w", err)
		}
		writer = w
	}

	count, err := e.writeRecords(writer, filtered, opts.Format)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("write records: %w", err)
	}

	if gzWriter != nil {
		gzWriter.Close()
	}
	if zipWriter != nil {
		zipWriter.Close()
	}
	tmpFile.Close()

	stat, _ := os.Stat(tmpPath)
	size := int64(0)
	if stat != nil {
		size = stat.Size()
	}

	slog.Info("log export completed",
		"records", count,
		"format", string(opts.Format),
		"size", size,
	)

	return &ExportResult{
		Path:     tmpPath,
		Records:  count,
		Size:     size,
		Duration: time.Since(start),
	}, nil
}

// writeRecords writes log records to the given writer.
func (e *Exporter) writeRecords(w io.Writer, records []logstream.LogRecord, format ExportFormat) (int, error) {
	switch format {
	case ExportCSV:
		return e.writeCSV(w, records)
	default: // jsonl, gz, zip all use JSONL
		return e.writeJSONL(w, records)
	}
}

// writeJSONL writes records as JSONL.
func (e *Exporter) writeJSONL(w io.Writer, records []logstream.LogRecord) (int, error) {
	count := 0
	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			continue
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			return count, fmt.Errorf("write jsonl: %w", err)
		}
		count++
	}
	return count, nil
}

// writeCSV writes records as CSV.
func (e *Exporter) writeCSV(w io.Writer, records []logstream.LogRecord) (int, error) {
	// Header
	w.Write([]byte("ts,level,component,source,msg,seq\n"))
	count := 0
	for _, rec := range records {
		msg := strings.ReplaceAll(rec.Msg, "\"", "\"\"")
		line := fmt.Sprintf("%s,%s,%s,%s,\"%s\",%d\n",
			rec.TS, rec.Level, rec.Component, rec.Source, msg, rec.Seq)
		if _, err := w.Write([]byte(line)); err != nil {
			return count, fmt.Errorf("write csv: %w", err)
		}
		count++
	}
	return count, nil
}

// filterRecords applies export filters to a slice of records.
func filterRecords(records []logstream.LogRecord, opts ExportOptions) []logstream.LogRecord {
	var result []logstream.LogRecord
	for _, rec := range records {
		if opts.Level != "" && rec.Level != opts.Level {
			continue
		}
		if opts.Component != "" && rec.Component != opts.Component {
			continue
		}
		if opts.Source != "" && rec.Source != opts.Source {
			continue
		}
		result = append(result, rec)
	}
	return result
}

// CleanupExport removes a temporary export file.
func CleanupExport(path string) {
	if path != "" && strings.HasPrefix(filepath.Base(path), "unet-export-") {
		os.Remove(path)
	}
}
