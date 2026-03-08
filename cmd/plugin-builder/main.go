package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/agentbrain/agentbrain/pkg/plugin"
)

const version = "1.0.0"

func main() {
	var (
		command    = flag.String("command", "", "Command to run: build, validate, template, package")
		pluginPath = flag.String("path", ".", "Path to plugin source code")
		outputDir  = flag.String("output", "./dist", "Output directory for built plugins")
		templateName = flag.String("template", "basic", "Template to use: basic, advanced")
		pluginName = flag.String("name", "", "Plugin name (required for template command)")
		verbose    = flag.Bool("verbose", false, "Enable verbose output")
		help       = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		showHelp()
		return
	}

	if *command == "" {
		fmt.Println("Error: command is required")
		showHelp()
		os.Exit(1)
	}

	builder := &PluginBuilder{
		PluginPath: *pluginPath,
		OutputDir:  *outputDir,
		Verbose:    *verbose,
	}

	switch *command {
	case "build":
		if err := builder.Build(); err != nil {
			log.Fatalf("Build failed: %v", err)
		}
		fmt.Println("Plugin built successfully")

	case "validate":
		if err := builder.Validate(); err != nil {
			log.Fatalf("Validation failed: %v", err)
		}
		fmt.Println("Plugin validation passed")

	case "template":
		if *pluginName == "" {
			log.Fatal("Plugin name is required for template command")
		}
		if err := builder.GenerateTemplate(*pluginName, *templateName); err != nil {
			log.Fatalf("Template generation failed: %v", err)
		}
		fmt.Printf("Template generated for plugin: %s\n", *pluginName)

	case "package":
		if err := builder.Package(); err != nil {
			log.Fatalf("Packaging failed: %v", err)
		}
		fmt.Println("Plugin packaged successfully")

	default:
		fmt.Printf("Unknown command: %s\n", *command)
		showHelp()
		os.Exit(1)
	}
}

// PluginBuilder handles plugin building operations
type PluginBuilder struct {
	PluginPath string
	OutputDir  string
	Verbose    bool
}

// Build compiles the plugin to a shared library
func (pb *PluginBuilder) Build() error {
	if pb.Verbose {
		fmt.Printf("Building plugin from: %s\n", pb.PluginPath)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(pb.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Find the main plugin file
	mainFile, err := pb.findMainFile()
	if err != nil {
		return fmt.Errorf("find main file: %w", err)
	}

	// Extract plugin name from metadata
	pluginName, err := pb.extractPluginName(mainFile)
	if err != nil {
		return fmt.Errorf("extract plugin name: %w", err)
	}

	// Build the shared library
	outputFile := filepath.Join(pb.OutputDir, pluginName+".so")
	
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", outputFile, mainFile)
	cmd.Dir = pb.PluginPath
	
	if pb.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Printf("Running: %s\n", cmd.String())
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}

	// Generate metadata file
	if err := pb.generateMetadata(pluginName); err != nil {
		return fmt.Errorf("generate metadata: %w", err)
	}

	if pb.Verbose {
		fmt.Printf("Plugin built: %s\n", outputFile)
	}

	return nil
}

// Validate validates the plugin code and metadata
func (pb *PluginBuilder) Validate() error {
	if pb.Verbose {
		fmt.Printf("Validating plugin: %s\n", pb.PluginPath)
	}

	// Find and parse the main file
	mainFile, err := pb.findMainFile()
	if err != nil {
		return fmt.Errorf("find main file: %w", err)
	}

	// Parse the Go source
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, mainFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse file: %w", err)
	}

	// Validate plugin structure
	if err := pb.validatePluginStructure(node); err != nil {
		return fmt.Errorf("validate structure: %w", err)
	}

	// Validate metadata
	if err := pb.validateMetadata(mainFile); err != nil {
		return fmt.Errorf("validate metadata: %w", err)
	}

	// Try to compile (dry run)
	if err := pb.testCompile(); err != nil {
		return fmt.Errorf("test compile: %w", err)
	}

	if pb.Verbose {
		fmt.Println("Plugin validation completed successfully")
	}

	return nil
}

// GenerateTemplate generates a plugin template
func (pb *PluginBuilder) GenerateTemplate(pluginName, templateType string) error {
	if pb.Verbose {
		fmt.Printf("Generating %s template for: %s\n", templateType, pluginName)
	}

	// Create plugin directory
	pluginDir := filepath.Join(pb.PluginPath, pluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("create plugin directory: %w", err)
	}

	// Generate files based on template type
	switch templateType {
	case "basic":
		return pb.generateBasicTemplate(pluginDir, pluginName)
	case "advanced":
		return pb.generateAdvancedTemplate(pluginDir, pluginName)
	default:
		return fmt.Errorf("unknown template type: %s", templateType)
	}
}

// Package creates a distributable package of the plugin
func (pb *PluginBuilder) Package() error {
	if pb.Verbose {
		fmt.Printf("Packaging plugin from: %s\n", pb.PluginPath)
	}

	// First build the plugin
	if err := pb.Build(); err != nil {
		return fmt.Errorf("build plugin: %w", err)
	}

	// Create package structure
	packageDir := filepath.Join(pb.OutputDir, "package")
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return fmt.Errorf("create package directory: %w", err)
	}

	// Copy binary and metadata
	pluginFiles, err := filepath.Glob(filepath.Join(pb.OutputDir, "*.so"))
	if err != nil {
		return fmt.Errorf("find plugin files: %w", err)
	}

	for _, pluginFile := range pluginFiles {
		destFile := filepath.Join(packageDir, filepath.Base(pluginFile))
		if err := pb.copyFile(pluginFile, destFile); err != nil {
			return fmt.Errorf("copy plugin file: %w", err)
		}
	}

	// Copy metadata files
	metadataFiles, err := filepath.Glob(filepath.Join(pb.OutputDir, "*.json"))
	if err != nil {
		return fmt.Errorf("find metadata files: %w", err)
	}

	for _, metadataFile := range metadataFiles {
		destFile := filepath.Join(packageDir, filepath.Base(metadataFile))
		if err := pb.copyFile(metadataFile, destFile); err != nil {
			return fmt.Errorf("copy metadata file: %w", err)
		}
	}

	// Create README
	if err := pb.generatePackageReadme(packageDir); err != nil {
		return fmt.Errorf("generate README: %w", err)
	}

	if pb.Verbose {
		fmt.Printf("Package created in: %s\n", packageDir)
	}

	return nil
}

// findMainFile finds the main plugin Go file
func (pb *PluginBuilder) findMainFile() (string, error) {
	// Look for main.go first
	mainFile := filepath.Join(pb.PluginPath, "main.go")
	if _, err := os.Stat(mainFile); err == nil {
		return mainFile, nil
	}

	// Look for any .go file with package main
	return pb.findPackageMainFile()
}

// findPackageMainFile finds a .go file with package main
func (pb *PluginBuilder) findPackageMainFile() (string, error) {
	err := filepath.Walk(pb.PluginPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Parse the file to check package name
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if err != nil {
			return nil // Skip files that can't be parsed
		}

		if node.Name.Name == "main" {
			return fmt.Errorf("found:%s", path) // Use error to return the path
		}

		return nil
	})

	if err != nil && strings.HasPrefix(err.Error(), "found:") {
		return strings.TrimPrefix(err.Error(), "found:"), nil
	}

	return "", fmt.Errorf("no main package file found in %s", pb.PluginPath)
}

// extractPluginName extracts the plugin name from the source code
func (pb *PluginBuilder) extractPluginName(mainFile string) (string, error) {
	// For now, use the directory name
	// In a full implementation, you would parse the metadata from the source
	dir := filepath.Dir(mainFile)
	return filepath.Base(dir), nil
}

// validatePluginStructure validates that the plugin implements required interfaces
func (pb *PluginBuilder) validatePluginStructure(node *ast.File) error {
	hasNewConnector := false
	hasPluginMetadata := false

	// Walk the AST to find required symbols
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			if x.Tok == token.VAR {
				for _, spec := range x.Specs {
					if valueSpec, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range valueSpec.Names {
							if name.Name == "PluginMetadata" {
								hasPluginMetadata = true
							}
						}
					}
				}
			}
		case *ast.FuncDecl:
			if x.Name.Name == "NewConnector" {
				hasNewConnector = true
			}
		}
		return true
	})

	if !hasNewConnector {
		return fmt.Errorf("plugin must export a NewConnector function")
	}

	if !hasPluginMetadata {
		return fmt.Errorf("plugin must export a PluginMetadata variable")
	}

	return nil
}

// validateMetadata validates plugin metadata
func (pb *PluginBuilder) validateMetadata(mainFile string) error {
	// This is a simplified validation
	// In a full implementation, you would extract and validate the metadata
	return nil
}

// testCompile tests that the plugin compiles without errors
func (pb *PluginBuilder) testCompile() error {
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", "/dev/null", ".")
	cmd.Dir = pb.PluginPath
	
	if pb.Verbose {
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

// generateMetadata generates metadata file for the plugin
func (pb *PluginBuilder) generateMetadata(pluginName string) error {
	// Create basic metadata
	metadata := plugin.PluginMetadata{
		Name:        pluginName,
		Version:     "1.0.0",
		Description: fmt.Sprintf("Auto-generated metadata for %s plugin", pluginName),
		Author:      "Unknown",
		License:     "MIT",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Write metadata file
	metadataFile := filepath.Join(pb.OutputDir, pluginName+".json")
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	return os.WriteFile(metadataFile, data, 0644)
}

// generateBasicTemplate generates a basic plugin template
func (pb *PluginBuilder) generateBasicTemplate(pluginDir, pluginName string) error {
	// Generate main.go
	mainTemplate := `package main

import (
	"context"
	"fmt"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/agentbrain/agentbrain/pkg/plugin"
)

// PluginMetadata is required for all plugins
var PluginMetadata = map[string]interface{}{
	"name":        "{{.Name}}",
	"version":     "1.0.0",
	"description": "{{.Description}}",
	"author":      "Your Name",
	"license":     "MIT",
	"capabilities": []string{"connector"},
}

// {{.ConnectorName}} implements the connector.Connector interface
type {{.ConnectorName}} struct {
	*plugin.BaseConnector
	// Add your connector-specific fields here
}

// NewConnector creates a new connector instance (required export)
func NewConnector(cfg *config.SourceConfig) (connector.Connector, error) {
	metadata := &plugin.PluginMetadata{
		Name:        "{{.Name}}",
		Version:     "1.0.0",
		Description: "{{.Description}}",
	}

	base := plugin.NewBaseConnector("{{.Name}}", metadata, cfg, nil)
	
	return &{{.ConnectorName}}{
		BaseConnector: base,
	}, nil
}

// Connect establishes a connection to the data source
func (c *{{.ConnectorName}}) Connect(ctx context.Context) error {
	// Implement your connection logic here
	return nil
}

// Close releases any resources held by the connector
func (c *{{.ConnectorName}}) Close() error {
	// Implement cleanup logic here
	return nil
}

// DiscoverMetadata returns metadata for all available objects
func (c *{{.ConnectorName}}) DiscoverMetadata(ctx context.Context) ([]connector.ObjectMetadata, error) {
	// Implement metadata discovery here
	return []connector.ObjectMetadata{}, nil
}

// DescribeObject returns detailed metadata for a specific object
func (c *{{.ConnectorName}}) DescribeObject(ctx context.Context, objectName string) (*connector.ObjectMetadata, error) {
	// Implement object description here
	return nil, fmt.Errorf("not implemented")
}

// GetIncrementalChanges streams records changed since the given watermark
func (c *{{.ConnectorName}}) GetIncrementalChanges(ctx context.Context, objectName string, watermarkField string, since time.Time) (<-chan connector.RecordBatch, <-chan error) {
	// Implement incremental sync here
	recordsCh := make(chan connector.RecordBatch)
	errCh := make(chan error)
	
	go func() {
		defer close(recordsCh)
		defer close(errCh)
		
		// Your incremental sync logic goes here
		errCh <- fmt.Errorf("not implemented")
	}()
	
	return recordsCh, errCh
}

// GetFullSnapshot streams all records for a full sync
func (c *{{.ConnectorName}}) GetFullSnapshot(ctx context.Context, objectName string) (<-chan connector.RecordBatch, <-chan error) {
	// Implement full sync here
	recordsCh := make(chan connector.RecordBatch)
	errCh := make(chan error)
	
	go func() {
		defer close(recordsCh)
		defer close(errCh)
		
		// Your full sync logic goes here
		errCh <- fmt.Errorf("not implemented")
	}()
	
	return recordsCh, errCh
}

// main is required for plugin compilation but should be empty
func main() {}
`

	tmpl, err := template.New("main").Parse(mainTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	mainFile := filepath.Join(pluginDir, "main.go")
	f, err := os.Create(mainFile)
	if err != nil {
		return fmt.Errorf("create main.go: %w", err)
	}
	defer f.Close()

	data := struct {
		Name          string
		Description   string
		ConnectorName string
	}{
		Name:          pluginName,
		Description:   fmt.Sprintf("A connector plugin for %s", pluginName),
		ConnectorName: fmt.Sprintf("%sConnector", strings.Title(pluginName)),
	}

	return tmpl.Execute(f, data)
}

// generateAdvancedTemplate generates an advanced plugin template
func (pb *PluginBuilder) generateAdvancedTemplate(pluginDir, pluginName string) error {
	// For now, just generate the basic template
	// In a full implementation, this would include more sophisticated features
	return pb.generateBasicTemplate(pluginDir, pluginName)
}

// copyFile copies a file from src to dst
func (pb *PluginBuilder) copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// generatePackageReadme generates a README for the package
func (pb *PluginBuilder) generatePackageReadme(packageDir string) error {
	readme := `# Plugin Package

This package contains a compiled AgentBrain plugin.

## Installation

1. Copy the .so file to your AgentBrain plugins directory
2. Copy the .json metadata file to the same directory
3. Restart AgentBrain or use the hot-reload API

## Configuration

Refer to the metadata file for configuration schema.

## Usage

Configure the plugin in your AgentBrain configuration file:

` + "```yaml" + `
sources:
  my_source:
    type: "plugin:plugin_name"
    auth:
      # Add authentication parameters
    options:
      # Add connector options
` + "```" + `

Generated by plugin-builder v` + version + `
`

	readmeFile := filepath.Join(packageDir, "README.md")
	return os.WriteFile(readmeFile, []byte(readme), 0644)
}

// showHelp displays help information
func showHelp() {
	fmt.Printf(`AgentBrain Plugin Builder v%s

Usage:
  plugin-builder -command=<command> [options]

Commands:
  build      Build plugin to shared library (.so)
  validate   Validate plugin code and structure
  template   Generate plugin template
  package    Create distributable plugin package

Options:
  -path      Path to plugin source code (default: ".")
  -output    Output directory (default: "./dist")
  -template  Template type: basic, advanced (default: "basic")
  -name      Plugin name (required for template command)
  -verbose   Enable verbose output
  -help      Show this help

Examples:
  plugin-builder -command=template -name=myconnector -template=basic
  plugin-builder -command=build -path=./myconnector -output=./dist
  plugin-builder -command=validate -path=./myconnector
  plugin-builder -command=package -path=./myconnector

`, version)
}