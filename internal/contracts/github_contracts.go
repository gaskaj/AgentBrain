package contracts

import (
	"time"
)

// GitHubContractSuite defines contracts for GitHub API endpoints
type GitHubContractSuite struct {
	contracts map[string]*APIContract
}

// NewGitHubContractSuite creates a new GitHub contract suite
func NewGitHubContractSuite() *GitHubContractSuite {
	suite := &GitHubContractSuite{
		contracts: make(map[string]*APIContract),
	}
	suite.initializeContracts()
	return suite
}

// GetContract returns a contract by name
func (g *GitHubContractSuite) GetContract(name string) *APIContract {
	return g.contracts[name]
}

// ListContracts returns all available contracts
func (g *GitHubContractSuite) ListContracts() []string {
	var names []string
	for name := range g.contracts {
		names = append(names, name)
	}
	return names
}

// initializeContracts sets up all GitHub API contracts
func (g *GitHubContractSuite) initializeContracts() {
	g.contracts["github-api"] = g.createGitHubAPIContract()
	g.contracts["github-repos"] = g.createReposContract()
	g.contracts["github-issues"] = g.createIssuesContract()
	g.contracts["github-pull-requests"] = g.createPullRequestsContract()
}

// createGitHubAPIContract creates the main GitHub API contract
func (g *GitHubContractSuite) createGitHubAPIContract() *APIContract {
	return &APIContract{
		Name:    "github-api",
		BaseURL: "https://api.github.com",
		Version: ContractVersion{
			Version:   "2022-11-28",
			CreatedAt: time.Now(),
			Description: "GitHub REST API v3 contract",
		},
		Headers: map[string]string{
			"Accept":     "application/vnd.github.v3+json",
			"User-Agent": "agentbrain/1.0",
		},
		Auth: AuthContract{
			Type: "bearer",
			Config: map[string]string{
				"token_header": "Authorization",
				"token_prefix": "Bearer",
			},
		},
		Endpoints: g.createGitHubEndpoints(),
	}
}

// createReposContract creates repository-specific contracts
func (g *GitHubContractSuite) createReposContract() *APIContract {
	return &APIContract{
		Name:    "github-repos",
		BaseURL: "https://api.github.com",
		Version: ContractVersion{
			Version:   "2022-11-28",
			CreatedAt: time.Now(),
			Description: "GitHub Repositories API contract",
		},
		Endpoints: g.createRepoEndpoints(),
	}
}

// createIssuesContract creates issues-specific contracts
func (g *GitHubContractSuite) createIssuesContract() *APIContract {
	return &APIContract{
		Name:    "github-issues",
		BaseURL: "https://api.github.com",
		Version: ContractVersion{
			Version:   "2022-11-28",
			CreatedAt: time.Now(),
			Description: "GitHub Issues API contract",
		},
		Endpoints: g.createIssueEndpoints(),
	}
}

// createPullRequestsContract creates pull requests-specific contracts
func (g *GitHubContractSuite) createPullRequestsContract() *APIContract {
	return &APIContract{
		Name:    "github-pull-requests",
		BaseURL: "https://api.github.com",
		Version: ContractVersion{
			Version:   "2022-11-28",
			CreatedAt: time.Now(),
			Description: "GitHub Pull Requests API contract",
		},
		Endpoints: g.createPullRequestEndpoints(),
	}
}

// createGitHubEndpoints creates the main GitHub API endpoints
func (g *GitHubContractSuite) createGitHubEndpoints() map[string]EndpointContract {
	return map[string]EndpointContract{
		"get-user": {
			Method:  "GET",
			Path:    "/user",
			Timeout: 5 * time.Second,
			ResponseSchemas: map[int]*JSONSchema{
				200: g.createUserSchema(),
				401: g.createErrorSchema(),
				403: g.createErrorSchema(),
			},
			Examples: []ExampleRequest{
				{
					Name: "get-authenticated-user",
					Description: "Get details about the authenticated user",
					Request: RequestExample{
						Headers: map[string]string{
							"Authorization": "Bearer token",
						},
					},
					Response: ResponseExample{
						StatusCode: 200,
						Body: map[string]interface{}{
							"login": "octocat",
							"id": 1,
							"name": "The Octocat",
							"email": "octocat@github.com",
							"public_repos": 8,
							"followers": 20,
							"following": 0,
						},
					},
				},
			},
		},
		"list-repos": {
			Method:  "GET",
			Path:    "/user/repos",
			Timeout: 10 * time.Second,
			QueryParams: map[string]ParamSpec{
				"type": {
					Type:        "string",
					Required:    false,
					Default:     "owner",
					EnumValues:  []string{"all", "owner", "public", "private", "member"},
					Description: "Repository type filter",
				},
				"sort": {
					Type:        "string",
					Required:    false,
					Default:     "full_name",
					EnumValues:  []string{"created", "updated", "pushed", "full_name"},
					Description: "Sort repositories by",
				},
				"per_page": {
					Type:        "integer",
					Required:    false,
					Default:     "30",
					Description: "Results per page (max 100)",
				},
			},
			ResponseSchemas: map[int]*JSONSchema{
				200: g.createRepositoryListSchema(),
				401: g.createErrorSchema(),
				403: g.createErrorSchema(),
			},
		},
	}
}

// createRepoEndpoints creates repository-specific endpoints
func (g *GitHubContractSuite) createRepoEndpoints() map[string]EndpointContract {
	return map[string]EndpointContract{
		"get-repo": {
			Method:  "GET",
			Path:    "/repos/{owner}/{repo}",
			Timeout: 5 * time.Second,
			ResponseSchemas: map[int]*JSONSchema{
				200: g.createRepositorySchema(),
				404: g.createErrorSchema(),
			},
		},
		"create-repo": {
			Method:  "POST",
			Path:    "/user/repos",
			Timeout: 10 * time.Second,
			RequestSchema: g.createCreateRepoSchema(),
			ResponseSchemas: map[int]*JSONSchema{
				201: g.createRepositorySchema(),
				400: g.createErrorSchema(),
				422: g.createValidationErrorSchema(),
			},
		},
	}
}

// createIssueEndpoints creates issue-specific endpoints
func (g *GitHubContractSuite) createIssueEndpoints() map[string]EndpointContract {
	return map[string]EndpointContract{
		"list-issues": {
			Method:  "GET",
			Path:    "/repos/{owner}/{repo}/issues",
			Timeout: 10 * time.Second,
			QueryParams: map[string]ParamSpec{
				"state": {
					Type:        "string",
					Required:    false,
					Default:     "open",
					EnumValues:  []string{"open", "closed", "all"},
					Description: "Issue state filter",
				},
				"labels": {
					Type:        "string",
					Required:    false,
					Description: "Comma-separated list of label names",
				},
				"sort": {
					Type:        "string",
					Required:    false,
					Default:     "created",
					EnumValues:  []string{"created", "updated", "comments"},
					Description: "Sort issues by",
				},
				"per_page": {
					Type:        "integer",
					Required:    false,
					Default:     "30",
					Description: "Results per page (max 100)",
				},
			},
			ResponseSchemas: map[int]*JSONSchema{
				200: g.createIssueListSchema(),
				404: g.createErrorSchema(),
			},
		},
		"create-issue": {
			Method:        "POST",
			Path:          "/repos/{owner}/{repo}/issues",
			Timeout:       10 * time.Second,
			RequestSchema: g.createCreateIssueSchema(),
			ResponseSchemas: map[int]*JSONSchema{
				201: g.createIssueSchema(),
				400: g.createErrorSchema(),
				422: g.createValidationErrorSchema(),
			},
		},
		"get-issue": {
			Method:  "GET",
			Path:    "/repos/{owner}/{repo}/issues/{issue_number}",
			Timeout: 5 * time.Second,
			ResponseSchemas: map[int]*JSONSchema{
				200: g.createIssueSchema(),
				404: g.createErrorSchema(),
			},
		},
	}
}

// createPullRequestEndpoints creates pull request-specific endpoints
func (g *GitHubContractSuite) createPullRequestEndpoints() map[string]EndpointContract {
	return map[string]EndpointContract{
		"list-pull-requests": {
			Method:  "GET",
			Path:    "/repos/{owner}/{repo}/pulls",
			Timeout: 10 * time.Second,
			QueryParams: map[string]ParamSpec{
				"state": {
					Type:        "string",
					Required:    false,
					Default:     "open",
					EnumValues:  []string{"open", "closed", "all"},
					Description: "Pull request state filter",
				},
				"sort": {
					Type:        "string",
					Required:    false,
					Default:     "created",
					EnumValues:  []string{"created", "updated", "popularity", "long-running"},
					Description: "Sort pull requests by",
				},
			},
			ResponseSchemas: map[int]*JSONSchema{
				200: g.createPullRequestListSchema(),
				404: g.createErrorSchema(),
			},
		},
		"create-pull-request": {
			Method:        "POST",
			Path:          "/repos/{owner}/{repo}/pulls",
			Timeout:       10 * time.Second,
			RequestSchema: g.createCreatePullRequestSchema(),
			ResponseSchemas: map[int]*JSONSchema{
				201: g.createPullRequestSchema(),
				422: g.createValidationErrorSchema(),
			},
		},
		"get-pull-request": {
			Method:  "GET",
			Path:    "/repos/{owner}/{repo}/pulls/{pull_number}",
			Timeout: 5 * time.Second,
			ResponseSchemas: map[int]*JSONSchema{
				200: g.createPullRequestSchema(),
				404: g.createErrorSchema(),
			},
		},
	}
}

// Schema creation methods

func (g *GitHubContractSuite) createUserSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"login":        {Type: "string"},
			"id":           {Type: "integer"},
			"name":         {Type: "string"},
			"email":        {Type: "string"},
			"public_repos": {Type: "integer"},
			"followers":    {Type: "integer"},
			"following":    {Type: "integer"},
			"created_at":   {Type: "string"},
			"updated_at":   {Type: "string"},
		},
		Required: []string{"login", "id"},
	}
}

func (g *GitHubContractSuite) createRepositorySchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"id":          {Type: "integer"},
			"name":        {Type: "string"},
			"full_name":   {Type: "string"},
			"description": {Type: "string"},
			"private":     {Type: "boolean"},
			"html_url":    {Type: "string"},
			"clone_url":   {Type: "string"},
			"created_at":  {Type: "string"},
			"updated_at":  {Type: "string"},
			"pushed_at":   {Type: "string"},
			"size":        {Type: "integer"},
			"language":    {Type: "string"},
			"owner":       g.createUserSchema(),
		},
		Required: []string{"id", "name", "full_name", "owner"},
	}
}

func (g *GitHubContractSuite) createRepositoryListSchema() *JSONSchema {
	return &JSONSchema{
		Type:  "array",
		Items: g.createRepositorySchema(),
	}
}

func (g *GitHubContractSuite) createIssueSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"id":         {Type: "integer"},
			"number":     {Type: "integer"},
			"title":      {Type: "string"},
			"body":       {Type: "string"},
			"state":      {Type: "string", Enum: []interface{}{"open", "closed"}},
			"user":       g.createUserSchema(),
			"assignee":   g.createUserSchema(),
			"labels":     g.createLabelListSchema(),
			"created_at": {Type: "string"},
			"updated_at": {Type: "string"},
			"closed_at":  {Type: "string"},
		},
		Required: []string{"id", "number", "title", "state", "user"},
	}
}

func (g *GitHubContractSuite) createIssueListSchema() *JSONSchema {
	return &JSONSchema{
		Type:  "array",
		Items: g.createIssueSchema(),
	}
}

func (g *GitHubContractSuite) createPullRequestSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"id":         {Type: "integer"},
			"number":     {Type: "integer"},
			"title":      {Type: "string"},
			"body":       {Type: "string"},
			"state":      {Type: "string", Enum: []interface{}{"open", "closed"}},
			"user":       g.createUserSchema(),
			"head":       g.createBranchSchema(),
			"base":       g.createBranchSchema(),
			"mergeable":  {Type: "boolean"},
			"created_at": {Type: "string"},
			"updated_at": {Type: "string"},
			"merged_at":  {Type: "string"},
		},
		Required: []string{"id", "number", "title", "state", "user", "head", "base"},
	}
}

func (g *GitHubContractSuite) createPullRequestListSchema() *JSONSchema {
	return &JSONSchema{
		Type:  "array",
		Items: g.createPullRequestSchema(),
	}
}

func (g *GitHubContractSuite) createLabelListSchema() *JSONSchema {
	return &JSONSchema{
		Type: "array",
		Items: &JSONSchema{
			Type: "object",
			Properties: map[string]*JSONSchema{
				"id":    {Type: "integer"},
				"name":  {Type: "string"},
				"color": {Type: "string"},
			},
			Required: []string{"id", "name", "color"},
		},
	}
}

func (g *GitHubContractSuite) createBranchSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"ref":  {Type: "string"},
			"sha":  {Type: "string"},
			"repo": g.createRepositorySchema(),
		},
		Required: []string{"ref", "sha"},
	}
}

func (g *GitHubContractSuite) createCreateRepoSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"name":        {Type: "string", MinLength: intPtr(1), MaxLength: intPtr(100)},
			"description": {Type: "string"},
			"private":     {Type: "boolean"},
			"auto_init":   {Type: "boolean"},
		},
		Required: []string{"name"},
	}
}

func (g *GitHubContractSuite) createCreateIssueSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"title":     {Type: "string", MinLength: intPtr(1)},
			"body":      {Type: "string"},
			"assignees": {Type: "array", Items: &JSONSchema{Type: "string"}},
			"labels":    {Type: "array", Items: &JSONSchema{Type: "string"}},
		},
		Required: []string{"title"},
	}
}

func (g *GitHubContractSuite) createCreatePullRequestSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"title": {Type: "string", MinLength: intPtr(1)},
			"body":  {Type: "string"},
			"head":  {Type: "string"},
			"base":  {Type: "string"},
		},
		Required: []string{"title", "head", "base"},
	}
}

func (g *GitHubContractSuite) createErrorSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"message":           {Type: "string"},
			"documentation_url": {Type: "string"},
		},
		Required: []string{"message"},
	}
}

func (g *GitHubContractSuite) createValidationErrorSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"message": {Type: "string"},
			"errors": {
				Type: "array",
				Items: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"resource": {Type: "string"},
						"field":    {Type: "string"},
						"code":     {Type: "string"},
					},
				},
			},
		},
		Required: []string{"message"},
	}
}

// Helper function for creating int pointers
func intPtr(i int) *int {
	return &i
}