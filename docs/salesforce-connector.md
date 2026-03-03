# Salesforce Connector

The Salesforce connector (`internal/connector/salesforce/`) is the first and reference connector implementation. It supports both REST API and Bulk API 2.0 for extracting data from Salesforce orgs.

## Authentication

Uses the OAuth 2.0 username-password flow. Required credentials:

| Config Key | Description |
|-----------|-------------|
| `client_id` | Salesforce Connected App consumer key |
| `client_secret` | Salesforce Connected App consumer secret |
| `username` | Salesforce user email |
| `password` | User password |
| `security_token` | User security token (appended to password) |
| `login_url` | Login endpoint (default: `https://login.salesforce.com`) |

```yaml
sources:
  salesforce_prod:
    type: salesforce
    auth:
      client_id: "${SF_CLIENT_ID}"
      client_secret: "${SF_CLIENT_SECRET}"
      username: "${SF_USERNAME}"
      password: "${SF_PASSWORD}"
      security_token: "${SF_SECURITY_TOKEN}"
      login_url: "https://login.salesforce.com"  # Use test.salesforce.com for sandboxes
```

The auth flow sends a POST to `{login_url}/services/oauth2/token` and receives an access token and instance URL.

## API Version

Uses Salesforce API version `v59.0`. Defined as the `apiVersion` constant in `client.go`.

## Metadata Discovery

### DescribeGlobal

Calls `GET /services/data/v59.0/sobjects/` to list all SObjects in the org.

Filters to queryable objects only. For each object, sets:
- `WatermarkField = "SystemModstamp"` for replicateable objects
- `WatermarkField = "LastModifiedDate"` for non-replicateable objects

### DescribeObject

Calls `GET /services/data/v59.0/sobjects/{object}/describe` for detailed field-level metadata.

Maps Salesforce field types to internal schema types:

| Salesforce Type | Internal FieldType |
|----------------|-------------------|
| `string`, `textarea`, `url`, `email`, `phone`, `picklist`, `multipicklist`, `combobox`, `reference`, `id`, `encryptedstring` | `string` |
| `int` | `integer` |
| `long` | `long` |
| `double`, `currency`, `percent` | `double` |
| `boolean` | `boolean` |
| `date` | `date` |
| `datetime` | `datetime` |
| `base64` | `binary` |
| Everything else | `string` (safe fallback) |

## Data Extraction

### REST API

Used for small result sets. Calls `GET /services/data/v59.0/query/?q={SOQL}`.

Features:
- Automatic pagination via `nextRecordsUrl`
- Strips Salesforce `attributes` key from each record
- Streams results via channel

### Bulk API 2.0

Used for large result sets. Three-phase flow:

1. **Create job:** `POST /services/data/v59.0/jobs/query` with SOQL query
2. **Poll status:** `GET /services/data/v59.0/jobs/query/{jobId}` until `JobComplete`
3. **Stream results:** `GET /services/data/v59.0/jobs/query/{jobId}/results` as CSV

Features:
- CSV streaming with configurable batch size (records are chunked into `RecordBatch`)
- Poll interval: 5 seconds
- Poll timeout: 30 minutes
- Handles job states: `InProgress`, `JobComplete`, `Failed`, `Aborted`
- Empty CSV values mapped to `nil`

### SOQL Generation

**Incremental query:**
```sql
SELECT {fields} FROM {object}
WHERE {watermarkField} > {timestamp}
ORDER BY {watermarkField} ASC
```

**Full snapshot query:**
```sql
SELECT {fields} FROM {object}
```

Fields are derived from `DescribeObject` schema field names (sorted).

## HTTP Client

The underlying HTTP client (`client.go`) provides:

- **Retry logic:** 3 attempts with exponential backoff (1s, 2s, 4s)
- **Retry conditions:** HTTP 429 (rate limit) and 5xx (server errors)
- **Non-retryable errors:** 4xx (except 429) result in immediate failure
- **Request timeout:** 2 minutes per HTTP request
- **Thread-safe token access:** `sync.RWMutex` protects access token

## Configuration Options

```yaml
sources:
  salesforce_prod:
    type: salesforce
    enabled: true
    schedule: "@every 1h"
    concurrency: 4        # Parallel object syncs
    batch_size: 10000     # Records per RecordBatch from Bulk API
    objects:              # Optional allow list (empty = all queryable objects)
      - Account
      - Contact
      - Opportunity
      - Lead
      - Case
    auth:
      client_id: "${SF_CLIENT_ID}"
      client_secret: "${SF_CLIENT_SECRET}"
      username: "${SF_USERNAME}"
      password: "${SF_PASSWORD}"
      security_token: "${SF_SECURITY_TOKEN}"
      login_url: "https://login.salesforce.com"
```

## File Structure

```
internal/connector/salesforce/
├── types.go       # API response structs (10 types)
├── client.go      # HTTP client, OAuth, retries
├── metadata.go    # DescribeGlobal, DescribeObject, type mapping
├── rest.go        # REST SOQL queries with pagination
├── bulk.go        # Bulk API 2.0: create → poll → stream CSV
└── connector.go   # SalesforceConnector (implements Connector), Register()
```

## Error Handling

- Authentication failures surface immediately (no retry)
- Transient HTTP errors (429, 5xx) are retried 3 times with exponential backoff
- Bulk job failures (state=Failed/Aborted) are returned as errors
- Context cancellation is checked between retries and during CSV streaming
- Schema discovery errors for individual objects are logged as warnings and the object is skipped

## Limitations

- Uses username-password OAuth flow only (no JWT bearer, no authorization code)
- No support for Salesforce Change Data Capture (CDC) events
- No Composite API support
- No support for binary/blob field content extraction
- Bulk API 2.0 only (no legacy Bulk API v1)
- No query locator caching between runs
