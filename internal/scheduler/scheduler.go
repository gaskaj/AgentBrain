package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"
)

// Job represents a scheduled sync job.
type Job struct {
	Name     string
	Schedule string
	RunFunc  func(ctx context.Context) error
}

// Scheduler manages cron-based job scheduling.
type Scheduler struct {
	cron   *cron.Cron
	logger *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	jobs   map[string]cron.EntryID
}

// New creates a new scheduler.
func New(logger *slog.Logger) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		cron: cron.New(cron.WithParser(cron.NewParser(
			cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
		))),
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
		jobs:   make(map[string]cron.EntryID),
	}
}

// AddJob schedules a job with the given cron expression.
func (s *Scheduler) AddJob(job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryID, err := s.cron.AddFunc(job.Schedule, func() {
		s.logger.Info("running scheduled job", "job", job.Name)
		if err := job.RunFunc(s.ctx); err != nil {
			s.logger.Error("scheduled job failed", "job", job.Name, "error", err)
		} else {
			s.logger.Info("scheduled job completed", "job", job.Name)
		}
	})
	if err != nil {
		return fmt.Errorf("schedule job %s: %w", job.Name, err)
	}

	s.jobs[job.Name] = entryID
	s.logger.Info("scheduled job", "job", job.Name, "schedule", job.Schedule)
	return nil
}

// Start begins the scheduler.
func (s *Scheduler) Start() {
	s.cron.Start()
	s.logger.Info("scheduler started")
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	s.cancel()
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.logger.Info("scheduler stopped")
}
