package storage

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	pq "github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/snappy"

	"github.com/agentbrain/agentbrain/internal/resource"
	"github.com/agentbrain/agentbrain/internal/schema"
)

// ParquetWriter writes dynamic record batches to Parquet files on S3.
type ParquetWriter struct {
	s3              *S3Client
	layout          Layout
	source          string
	logger          *slog.Logger
	resourceManager *resource.Manager
}

// NewParquetWriter creates a new ParquetWriter.
func NewParquetWriter(s3 *S3Client, source string, logger *slog.Logger) *ParquetWriter {
	return &ParquetWriter{
		s3:     s3,
		source: source,
		logger: logger,
	}
}

// SetResourceManager sets the resource manager for the Parquet writer
func (w *ParquetWriter) SetResourceManager(rm *resource.Manager) {
	w.resourceManager = rm
}

// WrittenFile contains info about a successfully written Parquet file.
type WrittenFile struct {
	Key      string
	Filename string
	Size     int64
	NumRows  int64
}

// WriteRecords writes a batch of records to a Parquet file on S3.
// Records are maps of field name to value. Returns info about the written file.
func (w *ParquetWriter) WriteRecords(ctx context.Context, objectName string, s *schema.Schema, records []map[string]any) (*WrittenFile, error) {
	if len(records) == 0 {
		return nil, nil
	}

	// Apply resource-aware batch size adjustment
	effectiveRecords := records
	if w.resourceManager != nil {
		if usage, err := w.resourceManager.GetCurrentUsage(ctx); err == nil {
			if usage.MemoryPercent > 80 {
				// Reduce batch size when memory usage is high
				maxRecords := len(records) / 2
				if maxRecords < 100 {
					maxRecords = 100 // Minimum batch size
				}
				if maxRecords < len(records) {
					effectiveRecords = records[:maxRecords]
					w.logger.Warn("reduced parquet batch size due to memory pressure",
						"original", len(records),
						"effective", len(effectiveRecords),
						"memory_percent", usage.MemoryPercent)
				}
			}
		}
	}

	pqSchema := schema.ToParquetSchema(s)

	filename := fmt.Sprintf("part-00000-%s.snappy.parquet", uuid.New().String())
	key := w.layout.ParquetFile(w.source, objectName, filename)

	buf := &bytes.Buffer{}
	writer := pq.NewGenericWriter[map[string]any](buf,
		pqSchema,
		pq.Compression(&snappy.Codec{}),
	)

	// Convert records to rows
	rows := make([]map[string]any, len(effectiveRecords))
	for i, rec := range effectiveRecords {
		row := make(map[string]any, len(rec))
		for k, v := range rec {
			row[k] = v
		}
		rows[i] = row
	}

	if _, err := writer.Write(rows); err != nil {
		return nil, fmt.Errorf("write parquet rows: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close parquet writer: %w", err)
	}

	data := buf.Bytes()
	if err := w.s3.Upload(ctx, key, data, "application/octet-stream"); err != nil {
		return nil, fmt.Errorf("upload parquet file: %w", err)
	}

	w.logger.Info("wrote parquet file",
		"object", objectName,
		"key", key,
		"rows", len(effectiveRecords),
		"bytes", len(data),
	)

	return &WrittenFile{
		Key:      key,
		Filename: filename,
		Size:     int64(len(data)),
		NumRows:  int64(len(effectiveRecords)),
	}, nil
}
