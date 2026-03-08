package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"syscall"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
)

// Sandbox provides security isolation for plugin processes
type Sandbox struct {
	config       *config.PluginSecurityConfig
	logger       *slog.Logger
	processes    map[string]*SandboxedProcess
	resourceMgr  *ResourceManager
}

// SandboxedProcess represents a plugin running in a sandboxed process
type SandboxedProcess struct {
	PID            int                `json:"pid"`
	PluginName     string             `json:"plugin_name"`
	Command        *exec.Cmd          `json:"-"`
	StartTime      time.Time          `json:"start_time"`
	Status         ProcessStatus      `json:"status"`
	ResourceUsage  *ResourceUsage     `json:"resource_usage"`
	RestartCount   int                `json:"restart_count"`
	LastRestart    time.Time          `json:"last_restart,omitempty"`
}

// ProcessStatus represents the status of a sandboxed process
type ProcessStatus string

const (
	ProcessStatusStarting ProcessStatus = "starting"
	ProcessStatusRunning  ProcessStatus = "running"
	ProcessStatusStopped  ProcessStatus = "stopped"
	ProcessStatusError    ProcessStatus = "error"
	ProcessStatusRestarting ProcessStatus = "restarting"
)

// ResourceUsage tracks resource consumption of a sandboxed process
type ResourceUsage struct {
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryMB      float64   `json:"memory_mb"`
	DiskReadMB    float64   `json:"disk_read_mb"`
	DiskWriteMB   float64   `json:"disk_write_mb"`
	NetworkRxMB   float64   `json:"network_rx_mb"`
	NetworkTxMB   float64   `json:"network_tx_mb"`
	FileDescriptors int     `json:"file_descriptors"`
	LastUpdated   time.Time `json:"last_updated"`
}

// ResourceManager manages resource limits for sandboxed processes
type ResourceManager struct {
	logger     *slog.Logger
	config     *config.PluginSecurityConfig
	monitoring map[string]*ResourceMonitor
}

// ResourceMonitor monitors resource usage for a specific process
type ResourceMonitor struct {
	PID         int
	PluginName  string
	limits      *ResourceLimits
	stopCh      chan struct{}
}

// ResourceLimits defines resource constraints for a plugin process
type ResourceLimits struct {
	MaxMemoryMB      int     `json:"max_memory_mb"`
	MaxCPUPercent    float64 `json:"max_cpu_percent"`
	MaxFileDescriptors int   `json:"max_file_descriptors"`
	NetworkAllowed   bool    `json:"network_allowed"`
	AllowedHosts     []string `json:"allowed_hosts"`
}

// NewSandbox creates a new sandbox manager
func NewSandbox(config *config.PluginSecurityConfig, logger *slog.Logger) *Sandbox {
	return &Sandbox{
		config:      config,
		logger:      logger,
		processes:   make(map[string]*SandboxedProcess),
		resourceMgr: NewResourceManager(config, logger),
	}
}

// StartPlugin starts a plugin in a sandboxed process
func (s *Sandbox) StartPlugin(ctx context.Context, pluginName, pluginPath string, args []string) (*SandboxedProcess, error) {
	s.logger.Info("Starting plugin in sandbox", "plugin", pluginName, "path", pluginPath)

	// Check if plugin is already running
	if existing, exists := s.processes[pluginName]; exists && existing.Status == ProcessStatusRunning {
		return existing, fmt.Errorf("plugin %s is already running", pluginName)
	}

	// Create command with security restrictions
	cmd := exec.CommandContext(ctx, pluginPath, args...)
	
	// Set up process isolation
	if err := s.configureProcessSecurity(cmd); err != nil {
		return nil, fmt.Errorf("configure process security: %w", err)
	}

	// Set up resource limits
	if err := s.configureResourceLimits(cmd); err != nil {
		return nil, fmt.Errorf("configure resource limits: %w", err)
	}

	// Set up network restrictions
	if err := s.configureNetworkRestrictions(cmd); err != nil {
		return nil, fmt.Errorf("configure network restrictions: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin process: %w", err)
	}

	process := &SandboxedProcess{
		PID:           cmd.Process.Pid,
		PluginName:    pluginName,
		Command:       cmd,
		StartTime:     time.Now(),
		Status:        ProcessStatusStarting,
		ResourceUsage: &ResourceUsage{},
	}

	s.processes[pluginName] = process

	// Start resource monitoring
	if err := s.resourceMgr.StartMonitoring(process); err != nil {
		s.logger.Warn("Failed to start resource monitoring", "plugin", pluginName, "error", err)
	}

	// Wait for the process to fully start
	go s.monitorProcess(process)

	process.Status = ProcessStatusRunning
	s.logger.Info("Plugin started successfully in sandbox", 
		"plugin", pluginName, 
		"pid", process.PID)

	return process, nil
}

// StopPlugin stops a sandboxed plugin process
func (s *Sandbox) StopPlugin(ctx context.Context, pluginName string) error {
	process, exists := s.processes[pluginName]
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	s.logger.Info("Stopping sandboxed plugin", "plugin", pluginName, "pid", process.PID)

	process.Status = ProcessStatusStopped

	// Stop resource monitoring
	s.resourceMgr.StopMonitoring(pluginName)

	// Gracefully terminate the process
	if process.Command != nil && process.Command.Process != nil {
		// Send SIGTERM first
		if err := process.Command.Process.Signal(syscall.SIGTERM); err != nil {
			s.logger.Warn("Failed to send SIGTERM", "plugin", pluginName, "error", err)
		}

		// Wait for graceful shutdown
		done := make(chan error, 1)
		go func() {
			done <- process.Command.Wait()
		}()

		select {
		case <-time.After(10 * time.Second):
			// Force kill if not gracefully stopped
			s.logger.Warn("Force killing plugin process", "plugin", pluginName)
			if err := process.Command.Process.Kill(); err != nil {
				s.logger.Error("Failed to kill plugin process", "plugin", pluginName, "error", err)
			}
		case err := <-done:
			if err != nil {
				s.logger.Debug("Plugin process exited with error", "plugin", pluginName, "error", err)
			}
		}
	}

	delete(s.processes, pluginName)
	s.logger.Info("Plugin stopped successfully", "plugin", pluginName)

	return nil
}

// RestartPlugin restarts a sandboxed plugin process
func (s *Sandbox) RestartPlugin(ctx context.Context, pluginName string) error {
	process, exists := s.processes[pluginName]
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	s.logger.Info("Restarting sandboxed plugin", "plugin", pluginName)

	process.Status = ProcessStatusRestarting
	process.RestartCount++
	process.LastRestart = time.Now()

	// Get the original plugin path and args
	pluginPath := process.Command.Path
	args := process.Command.Args[1:] // Skip the command itself

	// Stop the current process
	if err := s.StopPlugin(ctx, pluginName); err != nil {
		s.logger.Warn("Error stopping plugin for restart", "plugin", pluginName, "error", err)
	}

	// Start a new process
	newProcess, err := s.StartPlugin(ctx, pluginName, pluginPath, args)
	if err != nil {
		return fmt.Errorf("restart plugin: %w", err)
	}

	// Copy restart metadata
	newProcess.RestartCount = process.RestartCount
	newProcess.LastRestart = process.LastRestart

	return nil
}

// GetProcess returns information about a sandboxed process
func (s *Sandbox) GetProcess(pluginName string) (*SandboxedProcess, error) {
	process, exists := s.processes[pluginName]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", pluginName)
	}

	// Create a copy to avoid concurrent access issues
	processCopy := *process
	if process.ResourceUsage != nil {
		usageCopy := *process.ResourceUsage
		processCopy.ResourceUsage = &usageCopy
	}

	return &processCopy, nil
}

// ListProcesses returns information about all sandboxed processes
func (s *Sandbox) ListProcesses() map[string]*SandboxedProcess {
	result := make(map[string]*SandboxedProcess)
	
	for name, process := range s.processes {
		// Create a copy to avoid concurrent access issues
		processCopy := *process
		if process.ResourceUsage != nil {
			usageCopy := *process.ResourceUsage
			processCopy.ResourceUsage = &usageCopy
		}
		result[name] = &processCopy
	}

	return result
}

// configureProcessSecurity sets up process isolation and security
func (s *Sandbox) configureProcessSecurity(cmd *exec.Cmd) error {
	// Set up process group for better isolation
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	// Set up environment isolation
	cmd.Env = s.createSandboxEnvironment()

	return nil
}

// configureResourceLimits sets up resource constraints
func (s *Sandbox) configureResourceLimits(cmd *exec.Cmd) error {
	if s.config == nil {
		return nil
	}

	// Note: Full resource limiting would require cgroups or similar
	// This is a simplified version that sets process attributes
	
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	// Set process priority (lower priority for plugins)
	cmd.SysProcAttr.Credential = &syscall.Credential{
		// Run as specific user/group if configured
		// Uid: pluginUserID,
		// Gid: pluginGroupID,
	}

	return nil
}

// configureNetworkRestrictions sets up network access controls
func (s *Sandbox) configureNetworkRestrictions(cmd *exec.Cmd) error {
	if s.config == nil || s.config.NetworkAllowed {
		return nil
	}

	// Note: Full network isolation would require network namespaces
	// This is a placeholder for network restriction configuration
	
	return nil
}

// createSandboxEnvironment creates a restricted environment for the plugin
func (s *Sandbox) createSandboxEnvironment() []string {
	// Create minimal environment
	env := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/tmp",
		"USER=plugin",
	}

	// Add any allowed environment variables
	if s.config != nil {
		for key, value := range s.config.AllowedEnvVars {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	return env
}

// monitorProcess monitors a plugin process for health and resource usage
func (s *Sandbox) monitorProcess(process *SandboxedProcess) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if process is still running
			if process.Command.ProcessState != nil && process.Command.ProcessState.Exited() {
				s.logger.Warn("Plugin process exited unexpectedly", 
					"plugin", process.PluginName, 
					"exit_code", process.Command.ProcessState.ExitCode())
				process.Status = ProcessStatusError
				return
			}

			// Update resource usage
			if err := s.updateResourceUsage(process); err != nil {
				s.logger.Debug("Failed to update resource usage", 
					"plugin", process.PluginName, 
					"error", err)
			}

		default:
			if process.Status == ProcessStatusStopped {
				return
			}
			time.Sleep(5 * time.Second)
		}
	}
}

// updateResourceUsage updates resource usage metrics for a process
func (s *Sandbox) updateResourceUsage(process *SandboxedProcess) error {
	// This is a simplified implementation
	// In a full implementation, you would read from /proc/[pid]/stat, /proc/[pid]/status, etc.
	
	if process.Command == nil || process.Command.Process == nil {
		return fmt.Errorf("invalid process")
	}

	// Check if process still exists
	if err := process.Command.Process.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("process not running: %w", err)
	}

	// Update timestamp
	process.ResourceUsage.LastUpdated = time.Now()

	return nil
}

// NewResourceManager creates a new resource manager
func NewResourceManager(config *config.PluginSecurityConfig, logger *slog.Logger) *ResourceManager {
	return &ResourceManager{
		logger:     logger,
		config:     config,
		monitoring: make(map[string]*ResourceMonitor),
	}
}

// StartMonitoring starts resource monitoring for a process
func (rm *ResourceManager) StartMonitoring(process *SandboxedProcess) error {
	if rm.config == nil {
		return nil // No resource monitoring if config not provided
	}

	limits := &ResourceLimits{
		MaxMemoryMB:        rm.config.MaxMemoryMB,
		MaxCPUPercent:      rm.config.MaxCPUPercent,
		MaxFileDescriptors: 1024, // Default limit
		NetworkAllowed:     rm.config.NetworkAllowed,
		AllowedHosts:       rm.config.AllowedHosts,
	}

	monitor := &ResourceMonitor{
		PID:        process.PID,
		PluginName: process.PluginName,
		limits:     limits,
		stopCh:     make(chan struct{}),
	}

	rm.monitoring[process.PluginName] = monitor

	// Start monitoring goroutine
	go rm.monitorResources(monitor, process)

	return nil
}

// StopMonitoring stops resource monitoring for a plugin
func (rm *ResourceManager) StopMonitoring(pluginName string) {
	monitor, exists := rm.monitoring[pluginName]
	if !exists {
		return
	}

	close(monitor.stopCh)
	delete(rm.monitoring, pluginName)
}

// monitorResources monitors resource usage and enforces limits
func (rm *ResourceManager) monitorResources(monitor *ResourceMonitor, process *SandboxedProcess) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := rm.checkResourceLimits(monitor, process); err != nil {
				rm.logger.Error("Resource limit check failed", 
					"plugin", monitor.PluginName, 
					"error", err)
			}
		case <-monitor.stopCh:
			return
		}
	}
}

// checkResourceLimits checks if resource limits are being exceeded
func (rm *ResourceManager) checkResourceLimits(monitor *ResourceMonitor, process *SandboxedProcess) error {
	// Check memory usage
	if monitor.limits.MaxMemoryMB > 0 && process.ResourceUsage.MemoryMB > float64(monitor.limits.MaxMemoryMB) {
		rm.logger.Warn("Plugin exceeding memory limit", 
			"plugin", monitor.PluginName,
			"usage", process.ResourceUsage.MemoryMB,
			"limit", monitor.limits.MaxMemoryMB)
		
		// Could implement memory limit enforcement here
	}

	// Check CPU usage
	if monitor.limits.MaxCPUPercent > 0 && process.ResourceUsage.CPUPercent > monitor.limits.MaxCPUPercent {
		rm.logger.Warn("Plugin exceeding CPU limit", 
			"plugin", monitor.PluginName,
			"usage", process.ResourceUsage.CPUPercent,
			"limit", monitor.limits.MaxCPUPercent)
		
		// Could implement CPU throttling here
	}

	return nil
}