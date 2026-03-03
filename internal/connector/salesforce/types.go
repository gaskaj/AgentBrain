package salesforce

import "time"

// AuthConfig holds Salesforce OAuth configuration.
type AuthConfig struct {
	ClientID     string
	ClientSecret string
	Username     string
	Password     string
	SecurityToken string
	LoginURL     string
	AccessToken  string
	InstanceURL  string
	TokenExpiry  time.Time
}

// TokenResponse is the Salesforce OAuth token response.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	InstanceURL string `json:"instance_url"`
	ID          string `json:"id"`
	TokenType   string `json:"token_type"`
	IssuedAt    string `json:"issued_at"`
}

// DescribeGlobalResult is the response from /services/data/vXX.0/sobjects/.
type DescribeGlobalResult struct {
	SObjects []SObjectDescribe `json:"sobjects"`
}

// SObjectDescribe contains basic object metadata.
type SObjectDescribe struct {
	Name          string `json:"name"`
	Label         string `json:"label"`
	Queryable     bool   `json:"queryable"`
	Retrievable   bool   `json:"retrievable"`
	Replicateable bool   `json:"replicateable"`
	Createable    bool   `json:"createable"`
	Updateable    bool   `json:"updateable"`
	Custom        bool   `json:"custom"`
}

// DescribeResult is the response from /services/data/vXX.0/sobjects/{object}/describe.
type DescribeResult struct {
	Name   string          `json:"name"`
	Label  string          `json:"label"`
	Fields []FieldDescribe `json:"fields"`
}

// FieldDescribe contains field-level metadata.
type FieldDescribe struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Type       string `json:"type"`
	Length     int    `json:"length"`
	Nillable   bool   `json:"nillable"`
	Updateable bool   `json:"updateable"`
	Createable bool   `json:"createable"`
	SoapType   string `json:"soapType"`
}

// QueryResult is the response from a REST SOQL query.
type QueryResult struct {
	TotalSize      int              `json:"totalSize"`
	Done           bool             `json:"done"`
	NextRecordsURL string           `json:"nextRecordsUrl"`
	Records        []map[string]any `json:"records"`
}

// BulkJobRequest is the request body for creating a Bulk API 2.0 query job.
type BulkJobRequest struct {
	Operation string `json:"operation"`
	Query     string `json:"query"`
}

// BulkJobResponse is the response when creating/checking a Bulk API 2.0 job.
type BulkJobResponse struct {
	ID               string `json:"id"`
	Operation        string `json:"operation"`
	Object           string `json:"object"`
	State            string `json:"state"`
	NumberRecords    int    `json:"numberRecordsProcessed"`
	ErrorMessage     string `json:"errorMessage"`
	Retries          int    `json:"retries"`
	TotalProcessTime int64  `json:"totalProcessingTime"`
}

// BulkJobState constants.
const (
	BulkJobStateUploadComplete = "UploadComplete"
	BulkJobStateInProgress     = "InProgress"
	BulkJobStateAborted        = "Aborted"
	BulkJobStateFailed         = "Failed"
	BulkJobStateComplete       = "JobComplete"
)
