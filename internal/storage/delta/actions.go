package delta

import "time"

// Protocol action declares the minimum reader/writer versions.
type Protocol struct {
	MinReaderVersion int `json:"minReaderVersion"`
	MinWriterVersion int `json:"minWriterVersion"`
}

// Metadata action describes the table schema and configuration.
type Metadata struct {
	ID               string            `json:"id"`
	Name             string            `json:"name,omitempty"`
	Description      string            `json:"description,omitempty"`
	Format           Format            `json:"format"`
	SchemaString     string            `json:"schemaString"`
	PartitionColumns []string          `json:"partitionColumns"`
	Configuration    map[string]string `json:"configuration,omitempty"`
	CreatedTime      int64             `json:"createdTime,omitempty"`
}

// Format describes the data file format.
type Format struct {
	Provider string            `json:"provider"`
	Options  map[string]string `json:"options,omitempty"`
}

// Add action adds a data file to the table.
type Add struct {
	Path             string            `json:"path"`
	Size             int64             `json:"size"`
	PartitionValues  map[string]string `json:"partitionValues"`
	ModificationTime int64             `json:"modificationTime"`
	DataChange       bool              `json:"dataChange"`
	Stats            string            `json:"stats,omitempty"`
	Tags             map[string]string `json:"tags,omitempty"`
}

// Remove action logically deletes a data file from the table.
type Remove struct {
	Path                 string `json:"path"`
	DeletionTimestamp    int64  `json:"deletionTimestamp,omitempty"`
	DataChange           bool   `json:"dataChange"`
	ExtendedFileMetadata bool   `json:"extendedFileMetadata,omitempty"`
}

// CommitInfo action records provenance information about a commit.
type CommitInfo struct {
	Timestamp       int64             `json:"timestamp"`
	Operation       string            `json:"operation"`
	OperationParams map[string]string `json:"operationParameters,omitempty"`
	ReadVersion     int64             `json:"readVersion,omitempty"`
	IsBlindAppend   bool              `json:"isBlindAppend,omitempty"`
}

// Action wraps all possible Delta log actions. Each JSON line contains exactly one.
type Action struct {
	Protocol   *Protocol   `json:"protocol,omitempty"`
	MetaData   *Metadata   `json:"metaData,omitempty"`
	Add        *Add        `json:"add,omitempty"`
	Remove     *Remove     `json:"remove,omitempty"`
	CommitInfo *CommitInfo `json:"commitInfo,omitempty"`
}

// NewProtocolAction creates the initial protocol action.
func NewProtocolAction() Action {
	return Action{
		Protocol: &Protocol{
			MinReaderVersion: 1,
			MinWriterVersion: 2,
		},
	}
}

// NewMetadataAction creates a metadata action for a table.
func NewMetadataAction(id, name, schemaString string) Action {
	return Action{
		MetaData: &Metadata{
			ID:               id,
			Name:             name,
			Format:           Format{Provider: "parquet"},
			SchemaString:     schemaString,
			PartitionColumns: []string{},
			CreatedTime:      time.Now().UnixMilli(),
		},
	}
}

// NewAddAction creates an add action for a data file.
func NewAddAction(path string, size int64, stats string) Action {
	return Action{
		Add: &Add{
			Path:             path,
			Size:             size,
			PartitionValues:  map[string]string{},
			ModificationTime: time.Now().UnixMilli(),
			DataChange:       true,
			Stats:            stats,
		},
	}
}

// NewRemoveAction creates a remove action for a data file.
func NewRemoveAction(path string) Action {
	return Action{
		Remove: &Remove{
			Path:              path,
			DeletionTimestamp: time.Now().UnixMilli(),
			DataChange:        true,
		},
	}
}

// NewCommitInfoAction creates a commit info action.
func NewCommitInfoAction(operation string, readVersion int64, isBlindAppend bool) Action {
	return Action{
		CommitInfo: &CommitInfo{
			Timestamp:     time.Now().UnixMilli(),
			Operation:     operation,
			ReadVersion:   readVersion,
			IsBlindAppend: isBlindAppend,
		},
	}
}
