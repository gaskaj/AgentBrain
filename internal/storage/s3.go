package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/retry"
)

type S3Client struct {
	client            *s3.Client
	bucket            string
	prefix            string
	retryExecutor     *RetryExecutor
	circuitBreaker    *retry.CircuitBreaker
}

// RetryExecutor wraps retry policies and circuit breakers for S3 operations.
type RetryExecutor struct {
	uploadPolicy    *retry.RetryPolicy
	downloadPolicy  *retry.RetryPolicy
	circuitBreaker  *retry.CircuitBreaker
}

func NewS3Client(ctx context.Context, cfg config.StorageConfig) (*S3Client, error) {
	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(cfg.Region))

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	return &S3Client{
		client: client,
		bucket: cfg.Bucket,
		prefix: strings.TrimSuffix(cfg.Prefix, "/"),
	}, nil
}

// NewS3ClientWithCredentials creates an S3 client with explicit credentials (useful for testing).
func NewS3ClientWithCredentials(ctx context.Context, cfg config.StorageConfig, accessKey, secretKey, token string) (*S3Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, token)),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	s3Client := &S3Client{
		client: client,
		bucket: cfg.Bucket,
		prefix: strings.TrimSuffix(cfg.Prefix, "/"),
	}

	// Initialize retry executor with S3-optimized policies
	s3Client.initializeRetryPolicies()

	return s3Client, nil
}

func (c *S3Client) fullKey(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + "/" + key
}

// initializeRetryPolicies sets up retry policies and circuit breaker for S3 operations.
func (c *S3Client) initializeRetryPolicies() {
	// Create circuit breaker for S3 operations
	c.circuitBreaker = retry.NewCircuitBreaker("s3_operations", 5, 60*time.Second)

	// Create retry executor with S3-optimized policies
	uploadPolicy := retry.S3Policy()
	uploadPolicy.MaxAttempts = 5
	
	downloadPolicy := retry.S3Policy()
	downloadPolicy.MaxAttempts = 3

	c.retryExecutor = &RetryExecutor{
		uploadPolicy:   uploadPolicy,
		downloadPolicy: downloadPolicy,
		circuitBreaker: c.circuitBreaker,
	}
}

// SetRetryConfig allows customizing retry configuration for S3 operations.
func (c *S3Client) SetRetryConfig(retryConfig *config.RetryConfig) error {
	if retryConfig == nil {
		return nil
	}

	// Update upload policy if configured
	if uploadPolicy, exists := retryConfig.OperationPolicies["s3_upload"]; exists {
		policy, err := c.convertToPolicyConfig(uploadPolicy)
		if err != nil {
			return fmt.Errorf("invalid s3_upload policy: %w", err)
		}
		c.retryExecutor.uploadPolicy = policy
	}

	// Update download policy if configured
	if downloadPolicy, exists := retryConfig.OperationPolicies["s3_download"]; exists {
		policy, err := c.convertToPolicyConfig(downloadPolicy)
		if err != nil {
			return fmt.Errorf("invalid s3_download policy: %w", err)
		}
		c.retryExecutor.downloadPolicy = policy
	}

	// Update circuit breaker if configured
	if cbConfig, exists := retryConfig.CircuitBreakers["s3_operations"]; exists && cbConfig.Enabled {
		c.circuitBreaker = retry.NewCircuitBreaker("s3_operations", cbConfig.FailureThreshold, cbConfig.ResetTimeout)
		c.retryExecutor.circuitBreaker = c.circuitBreaker
	}

	return nil
}

// convertToPolicyConfig converts config.RetryPolicyConfig to retry.RetryPolicy.
func (c *S3Client) convertToPolicyConfig(config config.RetryPolicyConfig) (*retry.RetryPolicy, error) {
	policy := &retry.RetryPolicy{
		MaxAttempts: config.MaxAttempts,
		BaseDelay:   config.BaseDelay,
		MaxDelay:    config.MaxDelay,
		Jitter:      config.Jitter,
	}

	// Set backoff function
	switch config.BackoffStrategy {
	case "", "exponential":
		policy.BackoffFunc = retry.ExponentialBackoff
	case "exponential_jitter":
		policy.BackoffFunc = retry.ExponentialBackoffWithJitter
	case "linear":
		policy.BackoffFunc = retry.LinearBackoff
	case "fixed":
		policy.BackoffFunc = retry.FixedBackoff
	default:
		return nil, fmt.Errorf("unknown backoff strategy: %s", config.BackoffStrategy)
	}

	// Set retryable function for S3
	policy.RetryableFunc = retry.S3RetryableFunc

	return policy, nil
}

// GetCircuitBreakerMetrics returns metrics for the S3 circuit breaker.
func (c *S3Client) GetCircuitBreakerMetrics() retry.CircuitBreakerMetrics {
	if c.circuitBreaker == nil {
		return retry.CircuitBreakerMetrics{}
	}
	return c.circuitBreaker.GetMetrics()
}

func (c *S3Client) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	operation := func(ctx context.Context) (struct{}, error) {
		input := &s3.PutObjectInput{
			Bucket:      aws.String(c.bucket),
			Key:         aws.String(c.fullKey(key)),
			Body:        bytes.NewReader(data),
			ContentType: aws.String(contentType),
		}

		_, err := c.client.PutObject(ctx, input)
		if err != nil {
			return struct{}{}, fmt.Errorf("upload s3://%s/%s: %w", c.bucket, key, err)
		}
		return struct{}{}, nil
	}

	if c.retryExecutor != nil {
		_, err := retry.ExecuteWithRetryAndCircuitBreaker(ctx, c.retryExecutor.uploadPolicy, c.retryExecutor.circuitBreaker, operation)
		return err
	}

	// Fallback to direct execution if no retry executor
	_, err := operation(ctx)
	return err
}

func (c *S3Client) Download(ctx context.Context, key string) ([]byte, error) {
	operation := func(ctx context.Context) ([]byte, error) {
		input := &s3.GetObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(c.fullKey(key)),
		}

		resp, err := c.client.GetObject(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("download s3://%s/%s: %w", c.bucket, key, err)
		}
		defer resp.Body.Close()

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read s3://%s/%s: %w", c.bucket, key, err)
		}
		return data, nil
	}

	if c.retryExecutor != nil {
		return retry.ExecuteWithRetryAndCircuitBreaker(ctx, c.retryExecutor.downloadPolicy, c.retryExecutor.circuitBreaker, operation)
	}

	// Fallback to direct execution if no retry executor
	return operation(ctx)
}

func (c *S3Client) Exists(ctx context.Context, key string) (bool, error) {
	operation := func(ctx context.Context) (bool, error) {
		input := &s3.HeadObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(c.fullKey(key)),
		}

		_, err := c.client.HeadObject(ctx, input)
		if err != nil {
			var nsk *types.NotFound
			if ok := errors.As(err, &nsk); ok {
				return false, nil
			}
			// Also check for NoSuchKey
			var nf *types.NoSuchKey
			if ok := errors.As(err, &nf); ok {
				return false, nil
			}
			return false, fmt.Errorf("head s3://%s/%s: %w", c.bucket, key, err)
		}
		return true, nil
	}

	if c.retryExecutor != nil {
		return retry.ExecuteWithRetryAndCircuitBreaker(ctx, c.retryExecutor.downloadPolicy, c.retryExecutor.circuitBreaker, operation)
	}

	// Fallback to direct execution if no retry executor
	return operation(ctx)
}

func (c *S3Client) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := c.fullKey(prefix)
	var keys []string

	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(fullPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s: %w", c.bucket, prefix, err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if c.prefix != "" {
				key = strings.TrimPrefix(key, c.prefix+"/")
			}
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (c *S3Client) PutJSON(ctx context.Context, key string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON for %s: %w", key, err)
	}
	return c.Upload(ctx, key, data, "application/json")
}

func (c *S3Client) GetJSON(ctx context.Context, key string, v any) error {
	data, err := c.Download(ctx, key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal JSON from %s: %w", key, err)
	}
	return nil
}

func (c *S3Client) Delete(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.fullKey(key)),
	}

	_, err := c.client.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("delete s3://%s/%s: %w", c.bucket, key, err)
	}
	return nil
}

func (c *S3Client) UploadReader(ctx context.Context, key string, r io.Reader, contentType string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read data for %s: %w", key, err)
	}
	return c.Upload(ctx, key, data, contentType)
}

// ObjectMetadata represents metadata for an S3 object
type ObjectMetadata struct {
	Key           string
	Size          int64
	LastModified  time.Time
	ETag          string
	ContentType   string
}

// CopyObject copies an object from one location to another within S3
func (c *S3Client) CopyObject(ctx context.Context, sourceKey, destKey string) error {
	sourceURI := fmt.Sprintf("%s/%s", c.bucket, c.fullKey(sourceKey))
	
	input := &s3.CopyObjectInput{
		Bucket:     aws.String(c.bucket),
		Key:        aws.String(c.fullKey(destKey)),
		CopySource: aws.String(sourceURI),
	}

	_, err := c.client.CopyObject(ctx, input)
	if err != nil {
		return fmt.Errorf("copy s3://%s/%s to s3://%s/%s: %w", 
			c.bucket, sourceKey, c.bucket, destKey, err)
	}
	return nil
}

// CopyObjectToBucket copies an object to a different bucket
func (c *S3Client) CopyObjectToBucket(ctx context.Context, sourceKey, destBucket, destKey string) error {
	sourceURI := fmt.Sprintf("%s/%s", c.bucket, c.fullKey(sourceKey))
	
	input := &s3.CopyObjectInput{
		Bucket:     aws.String(destBucket),
		Key:        aws.String(destKey),
		CopySource: aws.String(sourceURI),
	}

	_, err := c.client.CopyObject(ctx, input)
	if err != nil {
		return fmt.Errorf("copy s3://%s/%s to s3://%s/%s: %w", 
			c.bucket, sourceKey, destBucket, destKey, err)
	}
	return nil
}

// ListObjectsWithMetadata returns a list of objects with their metadata
func (c *S3Client) ListObjectsWithMetadata(ctx context.Context, prefix string) ([]ObjectMetadata, error) {
	fullPrefix := c.fullKey(prefix)
	var objects []ObjectMetadata

	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(fullPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list objects s3://%s/%s: %w", c.bucket, prefix, err)
		}
		
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if c.prefix != "" {
				key = strings.TrimPrefix(key, c.prefix+"/")
			}
			
			metadata := ObjectMetadata{
				Key:          key,
				Size:         aws.ToInt64(obj.Size),
				LastModified: aws.ToTime(obj.LastModified),
				ETag:         aws.ToString(obj.ETag),
			}
			objects = append(objects, metadata)
		}
	}
	return objects, nil
}

// GetObjectMetadata retrieves metadata for a specific object
func (c *S3Client) GetObjectMetadata(ctx context.Context, key string) (*ObjectMetadata, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.fullKey(key)),
	}

	resp, err := c.client.HeadObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("get object metadata s3://%s/%s: %w", c.bucket, key, err)
	}

	metadata := &ObjectMetadata{
		Key:          key,
		Size:         aws.ToInt64(resp.ContentLength),
		LastModified: aws.ToTime(resp.LastModified),
		ETag:         aws.ToString(resp.ETag),
		ContentType:  aws.ToString(resp.ContentType),
	}

	return metadata, nil
}

// GetBucket returns the bucket name
func (c *S3Client) GetBucket() string {
	return c.bucket
}

// ListKeys returns a list of keys matching the prefix (alias for List)
func (c *S3Client) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	return c.List(ctx, prefix)
}
