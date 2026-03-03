package salesforce

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/agentbrain/agentbrain/internal/connector"
)

const (
	bulkPollInterval = 5 * time.Second
	bulkPollTimeout  = 30 * time.Minute
)

// BulkQuery executes a SOQL query via Bulk API 2.0 and streams results as RecordBatches.
func (c *Client) BulkQuery(ctx context.Context, soql string, objectName string, batchSize int, logger *slog.Logger) (<-chan connector.RecordBatch, <-chan error) {
	records := make(chan connector.RecordBatch, 4)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		// 1. Create the bulk query job
		jobReq := BulkJobRequest{
			Operation: "query",
			Query:     soql,
		}
		reqBody, _ := json.Marshal(jobReq)

		path := c.BaseURL() + "/jobs/query"
		respData, err := c.Post(ctx, path, strings.NewReader(string(reqBody)))
		if err != nil {
			errs <- fmt.Errorf("create bulk job: %w", err)
			return
		}

		var job BulkJobResponse
		if err := json.Unmarshal(respData, &job); err != nil {
			errs <- fmt.Errorf("decode bulk job response: %w", err)
			return
		}

		logger.Info("created bulk query job", "jobId", job.ID, "object", objectName)

		// 2. Poll until job completes
		if err := c.waitForBulkJob(ctx, job.ID, logger); err != nil {
			errs <- err
			return
		}

		// 3. Stream results
		if err := c.streamBulkResults(ctx, job.ID, objectName, batchSize, records); err != nil {
			errs <- err
			return
		}

		logger.Info("bulk query completed", "jobId", job.ID, "object", objectName)
	}()

	return records, errs
}

func (c *Client) waitForBulkJob(ctx context.Context, jobID string, logger *slog.Logger) error {
	deadline := time.Now().Add(bulkPollTimeout)
	path := fmt.Sprintf("%s/jobs/query/%s", c.BaseURL(), jobID)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("bulk job %s timed out", jobID)
		}

		data, err := c.Get(ctx, path)
		if err != nil {
			return fmt.Errorf("poll bulk job %s: %w", jobID, err)
		}

		var job BulkJobResponse
		if err := json.Unmarshal(data, &job); err != nil {
			return fmt.Errorf("decode bulk job status: %w", err)
		}

		switch job.State {
		case BulkJobStateComplete:
			logger.Info("bulk job completed", "jobId", jobID, "records", job.NumberRecords)
			return nil
		case BulkJobStateFailed:
			return fmt.Errorf("bulk job %s failed: %s", jobID, job.ErrorMessage)
		case BulkJobStateAborted:
			return fmt.Errorf("bulk job %s was aborted", jobID)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(bulkPollInterval):
		}
	}
}

func (c *Client) streamBulkResults(ctx context.Context, jobID, objectName string, batchSize int, out chan<- connector.RecordBatch) error {
	path := fmt.Sprintf("%s/jobs/query/%s/results", c.BaseURL(), jobID)

	body, err := c.GetStream(ctx, path)
	if err != nil {
		return fmt.Errorf("get bulk results: %w", err)
	}
	defer body.Close()

	reader := csv.NewReader(body)

	// Read header
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read CSV header: %w", err)
	}

	batch := make([]map[string]any, 0, batchSize)

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read CSV row: %w", err)
		}

		record := make(map[string]any, len(header))
		for i, field := range header {
			if i < len(row) {
				val := row[i]
				if val == "" {
					record[field] = nil
				} else {
					record[field] = val
				}
			}
		}
		batch = append(batch, record)

		if len(batch) >= batchSize {
			select {
			case out <- connector.RecordBatch{Records: batch, Object: objectName}:
			case <-ctx.Done():
				return ctx.Err()
			}
			batch = make([]map[string]any, 0, batchSize)
		}
	}

	// Send remaining records
	if len(batch) > 0 {
		select {
		case out <- connector.RecordBatch{Records: batch, Object: objectName}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
