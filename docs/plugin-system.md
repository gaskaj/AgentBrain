# Plugin System

AgentBrain's plugin system enables hot-reloadable connectors with security isolation and comprehensive management capabilities. This document provides an overview of the plugin architecture, configuration, and operational procedures.

## Overview

The plugin system consists of several key components:

- **Plugin Manager**: Core orchestration and lifecycle management
- **Plugin Connectors**: Wrapper layer implementing standard connector interface
- **Sandbox**: Security isolation for plugin processes
- **API Server**: REST endpoints for plugin management
- **Health Checker**: Monitoring and automatic recovery
- **SDK**: Development kit for plugin authors

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   AgentBrain Core                   │
├─────────────────────┬───────────────────────────────┤
│   Connector Registry │         Plugin Manager        │
├─────────────────────┼───────────────────────────────┤
│                     │   ┌─────────────────────────┐ │
│   Built-in          │   │    Plugin Connectors    │ │
│   Connectors        │   └─────────────────────────┘ │
│                     │   ┌─────────────────────────┐ │
│                     │   │      Sandbox Layer     │ │
│                     │   └─────────────────────────┘ │
│                     │   ┌─────────────────────────┐ │
│                     │   │    Health Checker      │ │
│                     │   └─────────────────────────┘ │
└─────────────────────┴───────────────────────────────┘
                      │
            ┌─────────┴─────────┐
            │  Plugin Processes │
            │  (.so libraries)  │
            └───────────────────┘
```

## Configuration

### Basic Plugin Configuration

```yaml
plugins:
  enabled: true
  directory: "/etc/agentbrain/plugins"
  auto_reload: true
  watch_paths:
    - "/etc/agentbrain/plugins"
    - "/opt/custom-plugins"
  security:
    sandbox_enabled: true
    max_memory_mb: 512
    max_cpu_percent: 25.0
    network_allowed: true
    allowed_hosts:
      - "api.salesforce.com"
      - "*.hubspot.com"
    allowed_env_vars:
      API_TIMEOUT: "30"
      LOG_LEVEL: "info"
```

### Source Configuration with Plugins

```yaml
sources:
  # Plugin-based connector
  salesforce_enhanced:
    type: "plugin:salesforce-enhanced"
    enabled: true
    auth:
      client_id: "${SFDC_CLIENT_ID}"
      client_secret: "${SFDC_CLIENT_SECRET}"
      username: "${SFDC_USERNAME}"
      password: "${SFDC_PASSWORD}"
      security_token: "${SFDC_SECURITY_TOKEN}"
    options:
      api_version: "v59.0"
      batch_size: 10000
      rate_limit: 5000
      
  # Built-in connector (fallback)
  salesforce_builtin:
    type: "salesforce"
    enabled: true
    # ... standard configuration
```

## Plugin Management

### REST API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/plugins` | List all loaded plugins |
| `GET` | `/api/v1/plugins/{name}` | Get plugin information |
| `POST` | `/api/v1/plugins/{name}/reload` | Hot-reload plugin |
| `POST` | `/api/v1/plugins/{name}/unload` | Unload plugin |
| `POST` | `/api/v1/plugins/load` | Load new plugin |
| `GET` | `/api/v1/plugins/{name}/health` | Get plugin health |
| `GET` | `/api/v1/plugins/{name}/metrics` | Get plugin metrics |
| `POST` | `/api/v1/plugins/{name}/restart` | Restart plugin process |

### Example API Usage

#### List Plugins

```bash
curl -X GET http://localhost:8080/api/v1/plugins
```

Response:
```json
{
  "plugins": {
    "salesforce-enhanced": {
      "name": "salesforce-enhanced",
      "version": "1.2.0",
      "status": "active",
      "load_time": "2024-01-15T10:30:00Z",
      "error_count": 0
    }
  },
  "total": 1
}
```

#### Hot-Reload Plugin

```bash
curl -X POST http://localhost:8080/api/v1/plugins/salesforce-enhanced/reload
```

Response:
```json
{
  "success": true,
  "message": "Plugin salesforce-enhanced reloaded successfully",
  "timestamp": "2024-01-15T10:35:00Z"
}
```

#### Check Plugin Health

```bash
curl -X GET http://localhost:8080/api/v1/plugins/salesforce-enhanced/health
```

Response:
```json
{
  "name": "salesforce-enhanced",
  "status": "active",
  "healthy": true,
  "last_check": "2024-01-15T10:34:30Z",
  "details": {
    "load_time": "2024-01-15T10:30:00Z",
    "version": "1.2.0",
    "error_count": 0
  }
}
```

## Security Model

### Sandboxing

Plugins run in isolated processes with configurable resource limits:

- **Memory Limits**: Prevent memory leaks from affecting the main process
- **CPU Limits**: Ensure fair resource allocation
- **Network Controls**: Restrict network access to approved hosts
- **File System**: Limited access to necessary directories only
- **Process Isolation**: Separate process groups for better isolation

### Resource Monitoring

The system continuously monitors plugin resource usage:

```yaml
security:
  max_memory_mb: 512        # Kill if exceeds 512MB
  max_cpu_percent: 25.0     # Throttle if exceeds 25% CPU
  network_allowed: true     # Allow network access
  allowed_hosts:            # Whitelist of allowed hosts
    - "api.example.com"
  sandbox_enabled: true     # Enable process sandboxing
```

### Permission Model

Plugins must declare required permissions:

```json
{
  "permissions": [
    {
      "type": "network",
      "resource": "api.salesforce.com",
      "actions": ["read", "write"],
      "description": "Access to Salesforce API"
    },
    {
      "type": "filesystem",
      "resource": "/tmp/plugin-cache",
      "actions": ["read", "write"],
      "description": "Temporary file storage"
    }
  ]
}
```

## Operational Procedures

### Plugin Deployment

1. **Development**: Create plugin using SDK and templates
2. **Build**: Compile to shared library (.so file)
3. **Package**: Create distribution package with metadata
4. **Deploy**: Copy to plugin directory
5. **Load**: Use API or auto-discovery to load plugin
6. **Verify**: Check health and metrics

### Hot Reload Process

1. **Trigger**: File change detection or API call
2. **Validation**: Verify plugin integrity and compatibility
3. **Graceful Unload**: Stop current plugin instance safely
4. **Load New Version**: Initialize new plugin version
5. **Health Check**: Verify new version is healthy
6. **Rollback**: Revert to previous version if issues detected

### Monitoring and Alerting

Key metrics to monitor:

- **Plugin Status**: Active, Error, Loading states
- **Resource Usage**: Memory, CPU, network I/O
- **Performance**: Response times, throughput
- **Error Rates**: Connection failures, data errors
- **Health Scores**: Overall plugin health assessment

### Troubleshooting

#### Common Issues

1. **Plugin Won't Load**
   - Check plugin directory permissions
   - Verify shared library format (.so)
   - Review plugin metadata and dependencies
   - Check logs for specific error messages

2. **High Resource Usage**
   - Monitor memory and CPU metrics
   - Review plugin implementation for leaks
   - Adjust resource limits if necessary
   - Consider plugin optimization

3. **Connection Failures**
   - Verify network access permissions
   - Check authentication credentials
   - Review firewall and proxy settings
   - Test external API connectivity

4. **Performance Degradation**
   - Monitor response times and throughput
   - Check for resource contention
   - Review plugin logging for bottlenecks
   - Consider scaling or optimization

#### Diagnostic Commands

```bash
# Check plugin status
curl -X GET http://localhost:8080/api/v1/plugins/plugin-name/health

# View plugin metrics
curl -X GET http://localhost:8080/api/v1/plugins/plugin-name/metrics

# Check logs
journalctl -u agentbrain -f --grep="plugin-name"

# Restart plugin process
curl -X POST http://localhost:8080/api/v1/plugins/plugin-name/restart
```

## Best Practices

### Plugin Development

1. **Use SDK**: Leverage provided SDK and base classes
2. **Error Handling**: Implement comprehensive error handling
3. **Resource Management**: Properly manage connections and resources
4. **Testing**: Include unit tests and integration tests
5. **Documentation**: Provide clear configuration documentation

### Deployment

1. **Staging**: Test plugins in staging environment first
2. **Gradual Rollout**: Deploy to subset of instances initially
3. **Monitoring**: Monitor health and performance closely
4. **Rollback Plan**: Have rollback procedures ready
5. **Version Control**: Track plugin versions and changes

### Operations

1. **Health Monitoring**: Set up alerts for plugin health issues
2. **Resource Limits**: Configure appropriate resource limits
3. **Security Updates**: Keep plugins updated for security
4. **Performance Tuning**: Regularly review and optimize performance
5. **Capacity Planning**: Monitor resource usage trends

## Migration from Built-in Connectors

### Migration Strategy

1. **Parallel Testing**: Run plugin alongside built-in connector
2. **Data Validation**: Compare outputs to ensure consistency
3. **Performance Testing**: Verify performance meets requirements
4. **Gradual Migration**: Migrate sources incrementally
5. **Monitoring**: Monitor for issues during migration

### Configuration Migration

```yaml
# Before (built-in)
sources:
  my_source:
    type: "salesforce"
    # ... configuration

# After (plugin)
sources:
  my_source:
    type: "plugin:salesforce-enhanced"
    # ... same configuration, potentially with additional options
```

## Compatibility

### AgentBrain Version Compatibility

Plugins specify minimum and maximum AgentBrain versions:

```json
{
  "requirements": {
    "min_agentbrain_version": "1.0.0",
    "max_agentbrain_version": "2.0.0"
  }
}
```

### API Stability

The plugin API follows semantic versioning:
- **Major**: Breaking changes to plugin interface
- **Minor**: New features, backward compatible
- **Patch**: Bug fixes, no API changes

## Support and Community

- **Documentation**: [Plugin Development Guide](plugin-development.md)
- **Examples**: See `examples/plugins/` directory
- **Issues**: Report bugs and feature requests on GitHub
- **Community**: Join our Discord for plugin development support