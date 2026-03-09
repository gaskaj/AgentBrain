# Security Architecture and Configuration

This document provides comprehensive information about AgentBrain's security framework, including configuration, monitoring, and best practices.

## Overview

AgentBrain implements a multi-layered security approach covering:

1. **Static Security Analysis** - Code-level security scanning using gosec and custom rules
2. **Dependency Vulnerability Management** - CVE tracking and package vulnerability scanning
3. **Runtime Security Monitoring** - Real-time detection of suspicious behavior
4. **Encryption and Transport Security** - TLS enforcement and credential protection
5. **Access Control and Authentication** - Security policy enforcement

## Architecture

The security framework is built around the `SecurityManager` which coordinates three main components:

```
SecurityManager
├── SecurityScanner (Static Analysis)
├── DependencyAuditor (CVE Scanning)
└── RuntimeMonitor (Runtime Security)
```

### Security Scanner

Performs static analysis using:
- **gosec**: Industry-standard Go security analyzer
- **Custom Rules**: Organization-specific security patterns
- **CWE Mapping**: Common Weakness Enumeration integration

### Dependency Auditor

Tracks vulnerabilities using:
- **govulncheck**: Go vulnerability database integration
- **CVE Tracking**: Common Vulnerabilities and Exposures monitoring
- **CVSS Scoring**: Risk assessment and prioritization

### Runtime Monitor

Monitors live systems for:
- Authentication failures and brute force attempts
- Memory and process anomalies
- Network connection anomalies
- File access violations
- TLS policy violations

## Configuration

### Basic Security Configuration

```yaml
security:
  enabled: true
  static_analysis:
    enabled: true
    fail_on_high: true
    exclude_rules: ["G104"]  # Skip specific gosec rules
    skip_directories: ["vendor", "node_modules", ".git"]
    custom_rules:
      - id: "CUSTOM001"
        name: "Hardcoded API Keys"
        description: "Detect hardcoded API keys in config"
        pattern: 'api_key\s*=\s*["\'][^"\']{20,}["\']'
        severity: "critical"
        cwe: "CWE-798"
        fix: "Use environment variables or secure vaults for API keys"
  
  dependency_audit:
    enabled: true
    fail_on_high: true
    check_interval: "24h"
    max_cvss_score: 7.0
    ignore_packages: ["test-package"]
    notify_channels: ["security-alerts"]
  
  runtime_monitoring:
    enabled: true
    auth_failure_threshold: 5
    network_anomaly_detection: true
    memory_anomaly_detection: true
    file_access_monitoring: true
    process_monitoring: true
  
  encryption:
    enforce_tls: true
    min_tls_version: "1.2"
    credential_encryption: true
    data_at_rest_encryption: true
    transit_encryption: true
```

### Advanced Configuration

#### Static Analysis Custom Rules

Custom rules use regular expression patterns to detect security issues:

```yaml
security:
  static_analysis:
    custom_rules:
      - id: "SQL_INJECTION"
        name: "SQL Injection Detection"
        description: "Detect potential SQL injection vulnerabilities"
        pattern: 'fmt\.Sprintf.*SELECT|INSERT|UPDATE|DELETE'
        severity: "high"
        cwe: "CWE-89"
        fix: "Use parameterized queries instead of string concatenation"
        tags: ["sql", "injection"]
        metadata:
          category: "injection"
          compliance: ["OWASP-A03"]
```

#### Runtime Monitoring Thresholds

Configure monitoring sensitivity:

```yaml
security:
  runtime_monitoring:
    auth_failure_threshold: 5        # Alert after 5 failed attempts
    memory_anomaly_detection: true   # Monitor memory usage patterns
    process_monitoring: true         # Track goroutine and process anomalies
    network_connection_tracking: true # Monitor network connections
```

#### Dependency Scanning Policies

Control vulnerability scanning behavior:

```yaml
security:
  dependency_audit:
    max_cvss_score: 7.0             # Ignore vulnerabilities below 7.0
    ignore_packages:                # Skip specific packages
      - "example.com/test-only"
      - "internal/dev-tools"
    notify_channels:                # Alert destinations
      - "security-alerts"
      - "dev-notifications"
```

## Security Rules and Standards

### Severity Levels

The security framework uses four severity levels:

- **Critical**: Immediate action required (CVSS 9.0-10.0)
- **High**: Urgent fix needed (CVSS 7.0-8.9)
- **Medium**: Fix in next cycle (CVSS 4.0-6.9)
- **Low**: Consider fixing (CVSS 0.1-3.9)

### Built-in Security Rules

AgentBrain includes built-in detection for:

| Rule ID | Description | Severity | CWE |
|---------|-------------|----------|-----|
| G101 | Hardcoded credentials | High | CWE-798 |
| G102 | Bind to all interfaces | Medium | CWE-362 |
| G204 | Command execution | High | CWE-78 |
| G401 | Weak cryptographic primitives | High | CWE-327 |
| G402 | Bad TLS connection settings | High | CWE-295 |
| G404 | Insecure random number source | High | CWE-338 |

### Custom Rule Development

Create organization-specific rules:

```yaml
custom_rules:
  - id: "AGENTBRAIN_001"
    name: "Internal API Authentication"
    description: "Ensure internal APIs use proper authentication"
    pattern: 'http\.HandleFunc.*\/internal\/.*'
    severity: "medium"
    fix: "Add authentication middleware to internal API endpoints"
    tags: ["api", "authentication", "internal"]
```

## Security Monitoring and Alerting

### Real-time Monitoring

The runtime monitor tracks security metrics:

```go
type SecurityMetrics struct {
    FailedAuthAttempts      int64   // Authentication failures
    UnexpectedNetworkIO     int64   // Suspicious network activity
    SuspiciousFileAccess    int64   // Unauthorized file operations
    MemoryAnomalies         int64   // Memory usage anomalies
    ProcessAnomalies        int64   // Process/goroutine anomalies
    TLSViolations          int64   // TLS policy violations
    CredentialExposures    int64   // Credential leak events
}
```

### Alert Configuration

Configure alerting thresholds and channels:

```yaml
security:
  runtime_monitoring:
    auth_failure_threshold: 5
    alert_channels:
      - type: "slack"
        webhook: "${SLACK_WEBHOOK_URL}"
        channel: "#security-alerts"
      - type: "email"
        recipients: ["security@company.com"]
      - type: "webhook"
        url: "https://siem.company.com/api/alerts"
```

### Security Scoring

AgentBrain calculates security scores (0-100) based on:

- Static analysis findings
- Vulnerability count and severity
- Runtime security metrics
- Configuration compliance

## Compliance and Standards

### Supported Frameworks

The security framework supports compliance with:

- **SOC 2 Type II**: Security monitoring and controls
- **ISO 27001**: Information security management
- **OWASP Top 10**: Web application security risks
- **NIST Cybersecurity Framework**: Security controls and monitoring

### Compliance Mapping

| Control | Implementation |
|---------|----------------|
| Access Control | Runtime authentication monitoring |
| Encryption | TLS enforcement, credential encryption |
| Vulnerability Management | Automated scanning and tracking |
| Security Monitoring | Real-time threat detection |
| Incident Response | Automated alerting and reporting |

## Best Practices

### Development Security

1. **Enable security scanning** in development environment
2. **Use pre-commit hooks** for security checks
3. **Review security reports** before deployment
4. **Implement security tests** in CI/CD pipeline

### Operational Security

1. **Monitor security metrics** continuously
2. **Set appropriate alert thresholds** to avoid noise
3. **Review security reports** regularly
4. **Update dependencies** promptly when vulnerabilities are found
5. **Rotate credentials** on a regular schedule

### Configuration Security

1. **Enable all security features** appropriate for your environment
2. **Use environment variables** for sensitive configuration
3. **Implement least privilege access** controls
4. **Regular security configuration reviews**

### Example Pre-commit Hook

```bash
#!/bin/bash
# .git/hooks/pre-commit
echo "Running security checks..."

# Run gosec
if command -v gosec >/dev/null 2>&1; then
    gosec -quiet ./...
    if [ $? -ne 0 ]; then
        echo "Security issues found! Fix before committing."
        exit 1
    fi
fi

# Run govulncheck
if command -v govulncheck >/dev/null 2>&1; then
    govulncheck ./...
    if [ $? -ne 0 ]; then
        echo "Vulnerabilities found! Update dependencies before committing."
        exit 1
    fi
fi

echo "Security checks passed!"
```

## Troubleshooting

### Common Issues

#### High Memory Usage Alerts

If memory anomaly alerts are frequent:

1. Check for memory leaks in application code
2. Review goroutine count for potential leaks
3. Adjust memory thresholds if alerts are false positives
4. Monitor memory usage trends over time

#### False Positive Security Rules

To reduce false positives:

1. Add specific exclusions to `exclude_rules`
2. Adjust custom rule patterns to be more specific
3. Use `skip_directories` to exclude test or vendor code
4. Review severity levels for internal tools

#### Dependency Scan Failures

If dependency scanning fails:

1. Ensure `govulncheck` is installed and accessible
2. Check network connectivity to vulnerability databases
3. Review `ignore_packages` configuration
4. Update vulnerability database: `govulncheck -update`

### Performance Tuning

#### Scanner Performance

- Use `skip_directories` to exclude large vendor directories
- Limit custom rules to essential patterns only
- Run scans during off-peak hours for large codebases

#### Runtime Monitoring Performance

- Adjust monitoring intervals based on system load
- Use sampling for high-frequency events
- Configure appropriate buffer sizes for event storage

## Security Event Response

### Incident Response Workflow

1. **Detection**: Security event triggers alert
2. **Triage**: Analyze event severity and impact
3. **Investigation**: Gather additional context and evidence
4. **Response**: Take appropriate remediation action
5. **Recovery**: Restore normal operations
6. **Review**: Post-incident analysis and improvement

### Automated Response

Configure automated responses for common scenarios:

```yaml
security:
  runtime_monitoring:
    auto_response:
      auth_failures:
        threshold: 10
        action: "block_ip"
        duration: "1h"
      credential_exposure:
        action: "rotate_credentials"
        notify: ["security@company.com"]
```

## Integration with External Tools

### SIEM Integration

AgentBrain can forward security events to SIEM systems:

```yaml
security:
  integrations:
    siem:
      enabled: true
      endpoint: "https://siem.company.com/api/events"
      format: "json"
      authentication:
        type: "bearer_token"
        token: "${SIEM_API_TOKEN}"
```

### Vulnerability Scanners

Integrate with external vulnerability scanners:

```yaml
security:
  integrations:
    scanners:
      - name: "snyk"
        type: "dependency"
        config:
          api_key: "${SNYK_API_KEY}"
          org: "my-org"
      - name: "sonarqube"
        type: "static"
        config:
          url: "https://sonar.company.com"
          token: "${SONAR_TOKEN}"
```

## API Reference

For detailed API information, see the security package documentation:

- `SecurityManager`: Main security orchestration
- `SecurityScanner`: Static analysis interface
- `DependencyAuditor`: Vulnerability scanning
- `RuntimeMonitor`: Runtime security monitoring

## Further Reading

- [Vulnerability Management Guide](vulnerability-management.md)
- [Configuration Reference](configuration.md)
- [Monitoring and Alerting](monitoring.md)
- [OWASP Go Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Go_Security_Cheat_Sheet.html)
- [gosec Documentation](https://securecodewarrior.github.io/gosec/)