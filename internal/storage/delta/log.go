package delta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// S3Store is the interface needed by the delta log for storage operations.
type S3Store interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) error
	Download(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	PutJSON(ctx context.Context, key string, v any) error
	GetJSON(ctx context.Context, key string, v any) error
	Exists(ctx context.Context, key string) (bool, error)
}

// TransactionLog handles reading and writing Delta transaction log entries.
type TransactionLog struct {
	store     S3Store
	logPrefix string
}

// NewTransactionLog creates a new transaction log for the given delta log prefix.
func NewTransactionLog(store S3Store, logPrefix string) *TransactionLog {
	return &TransactionLog{
		store:     store,
		logPrefix: logPrefix,
	}
}

// WriteVersion writes a set of actions to a specific version file.
func (l *TransactionLog) WriteVersion(ctx context.Context, version int64, actions []Action) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, a := range actions {
		if err := enc.Encode(a); err != nil {
			return fmt.Errorf("encode action: %w", err)
		}
	}

	key := l.versionKey(version)
	if err := l.store.Upload(ctx, key, buf.Bytes(), "application/json"); err != nil {
		return fmt.Errorf("write version %d: %w", version, err)
	}
	return nil
}

// ReadVersion reads all actions from a specific version file.
func (l *TransactionLog) ReadVersion(ctx context.Context, version int64) ([]Action, error) {
	key := l.versionKey(version)
	data, err := l.store.Download(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read version %d: %w", version, err)
	}
	return decodeActions(data)
}

// ListVersions returns all version numbers in the log, sorted ascending.
func (l *TransactionLog) ListVersions(ctx context.Context) ([]int64, error) {
	keys, err := l.store.List(ctx, l.logPrefix)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}

	var versions []int64
	for _, key := range keys {
		base := key
		if idx := strings.LastIndex(key, "/"); idx >= 0 {
			base = key[idx+1:]
		}
		if !strings.HasSuffix(base, ".json") || strings.HasPrefix(base, "_") {
			continue
		}
		numStr := strings.TrimSuffix(base, ".json")
		v, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions, nil
}

// LatestVersion returns the highest version number, or -1 if no versions exist.
func (l *TransactionLog) LatestVersion(ctx context.Context) (int64, error) {
	versions, err := l.ListVersions(ctx)
	if err != nil {
		return -1, err
	}
	if len(versions) == 0 {
		return -1, nil
	}
	return versions[len(versions)-1], nil
}

func (l *TransactionLog) versionKey(version int64) string {
	return fmt.Sprintf("%s%020d.json", l.logPrefix, version)
}

func decodeActions(data []byte) ([]Action, error) {
	var actions []Action
	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var a Action
		if err := dec.Decode(&a); err != nil {
			return nil, fmt.Errorf("decode action: %w", err)
		}
		actions = append(actions, a)
	}
	return actions, nil
}
