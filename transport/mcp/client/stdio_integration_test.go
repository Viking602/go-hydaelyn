package mcpclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Viking602/go-hydaelyn/message"
)

const (
	stdioHelperModeEnv    = "HYDAELYN_MCP_STDIO_HELPER_MODE"
	stdioHandshakeFileEnv = "HYDAELYN_MCP_STDIO_HANDSHAKE_FILE"
	stdioCloseFileEnv     = "HYDAELYN_MCP_STDIO_CLOSE_FILE"
	stdioParentSecretEnv  = "HYDAELYN_MCP_STDIO_PARENT_SECRET"
	stdioOversizeModeEnv  = "HYDAELYN_MCP_STDIO_OVERSIZE"
	stdioLargeResponseLen = 2 << 20
)

func TestMain(m *testing.M) {
	switch os.Getenv(stdioHelperModeEnv) {
	case "official":
		runOfficialStdioServer()
		os.Exit(0)
	case "unsupported":
		runUnsupportedStdioServer()
		os.Exit(0)
	case "large-exit":
		runLargeExitStdioServer()
		os.Exit(0)
	case "pretty-init":
		runPrettyInitializeStdioServer()
		os.Exit(0)
	default:
		os.Exit(m.Run())
	}
}

func TestDialStdioCompletesOfficialHandshakeBeforeToolCalls(t *testing.T) {
	// Given
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	handshakeFile := filepath.Join(t.TempDir(), "handshake.json")
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Args:    []string{"configured-argument"},
		Env: []string{
			stdioHelperModeEnv + "=official",
			stdioHandshakeFileEnv + "=" + handshakeFile,
		},
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// When
	initialized, err := client.Initialize(context.Background(), "stdio-client", "v1.0.0")
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	result, err := client.CallTool(context.Background(), "ready", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}

	// Then
	if initialized.ProtocolVersion == "" || initialized.ServerInfo.Name != "stdio-server" {
		t.Fatalf("Initialize() = %#v", initialized)
	}
	if len(tools) != 1 || tools[0].Name != "ready" {
		t.Fatalf("ListTools() = %#v", tools)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "initialized" {
		t.Fatalf("CallTool() = %#v", result)
	}
	var handshake stdioHandshake
	readJSONFile(t, handshakeFile, &handshake)
	if handshake.ProtocolVersion == "" || handshake.Capabilities == 0 {
		t.Fatalf("handshake = %#v", handshake)
	}
	if len(handshake.Arguments) != 1 || handshake.Arguments[0] != "configured-argument" {
		t.Fatalf("stdio arguments = %#v, want configured-argument", handshake.Arguments)
	}
}

func TestDialStdioHonorsDir(t *testing.T) {
	// Given
	t.Setenv(stdioParentSecretEnv, "parent-secret")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	workDir := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	handshakeFile := filepath.Join(t.TempDir(), "handshake.json")
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Dir:     workDir,
		Env: []string{
			stdioHelperModeEnv + "=official",
			stdioHandshakeFileEnv + "=" + handshakeFile,
		},
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	if _, err := client.Initialize(context.Background(), "stdio-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := client.CallTool(context.Background(), "ready", nil); err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// When
	var handshake stdioHandshake
	readJSONFile(t, handshakeFile, &handshake)

	// Then
	actualInfo, err := os.Stat(handshake.WorkingDirectory)
	if err != nil {
		t.Fatalf("Stat(actual dir) error = %v", err)
	}
	wantInfo, err := os.Stat(workDir)
	if err != nil {
		t.Fatalf("Stat(want dir) error = %v", err)
	}
	if !os.SameFile(actualInfo, wantInfo) {
		t.Fatalf("working directory = %q, want same file as %q", handshake.WorkingDirectory, workDir)
	}
	otherInfo, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatalf("Stat(other dir) error = %v", err)
	}
	if os.SameFile(actualInfo, otherInfo) {
		t.Fatalf("working directory = %q, unexpectedly same file as second directory", handshake.WorkingDirectory)
	}
	if handshake.ParentSecret != "" {
		t.Fatalf("parent secret leaked to child: %q", handshake.ParentSecret)
	}
}

func TestDialStdioInheritEnvIncludesParentEnvironment(t *testing.T) {
	// Given
	t.Setenv(stdioParentSecretEnv, "parent-ok")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	handshakeFile := filepath.Join(t.TempDir(), "handshake.json")
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Env: []string{
			stdioHelperModeEnv + "=official",
			stdioHandshakeFileEnv + "=" + handshakeFile,
		},
		InheritEnv: true,
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	if _, err := client.Initialize(context.Background(), "stdio-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := client.CallTool(context.Background(), "ready", nil); err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// When
	var handshake stdioHandshake
	readJSONFile(t, handshakeFile, &handshake)

	// Then
	if handshake.ParentSecret != "parent-ok" {
		t.Fatalf("inherited parent secret = %q, want parent-ok", handshake.ParentSecret)
	}
}

func TestDialStdioUnsupportedProtocolClosesProcess(t *testing.T) {
	// Given
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	temp := t.TempDir()
	handshakeFile := filepath.Join(temp, "wire.json")
	closeFile := filepath.Join(temp, "closed")
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Env: []string{
			stdioHelperModeEnv + "=unsupported",
			stdioHandshakeFileEnv + "=" + handshakeFile,
			stdioCloseFileEnv + "=" + closeFile,
		},
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}

	// When
	_, err = client.Initialize(context.Background(), "stdio-client", "v1.0.0")

	// Then
	if err == nil {
		t.Fatal("Initialize() error = nil, want unsupported protocol error")
	}
	var wire stdioWireObservation
	readJSONFile(t, handshakeFile, &wire)
	if wire.FirstMethod != "initialize" || wire.Capabilities == 0 || wire.HadContentLength {
		t.Fatalf("wire observation = %#v", wire)
	}
	if _, statErr := os.Stat(closeFile); statErr != nil {
		t.Fatalf("stdio process was not closed after negotiation failure: %v", statErr)
	}
}

func TestDialStdioRejectsOversizeMessageAndWaitsForProcessExit(t *testing.T) {
	// Given
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	closeFile := filepath.Join(t.TempDir(), "closed")
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Env: []string{
			stdioHelperModeEnv + "=official",
			stdioOversizeModeEnv + "=1",
			stdioCloseFileEnv + "=" + closeFile,
		},
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}
	if _, err := client.Initialize(context.Background(), "stdio-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// When
	_, callErr := client.CallTool(context.Background(), "oversize", nil)
	closeErr := client.Close()

	// Then
	assertDefaultInboundLimitError(t, callErr)
	if closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if _, err := os.Stat(closeFile); err != nil {
		t.Fatalf("stdio process did not exit before Close returned: %v", err)
	}
}

func TestCommandIOTransportDrainsLargeResponseAfterProcessExit(t *testing.T) {
	// Given
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	command := exec.Command(executable)
	command.Env = []string{stdioHelperModeEnv + "=large-exit"}
	transport := newCommandIOTransport(command, defaultStdioCloseTimeout)
	client := New(transport)
	if _, err := client.Initialize(context.Background(), "stdio-client", "v1.0.0"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// When
	tools, listErr := client.ListTools(context.Background())
	closeErr := client.Close()
	repeatedCloseErr := transport.Close()

	// Then
	if listErr != nil {
		t.Fatalf("ListTools() error = %v", listErr)
	}
	if len(tools) != 1 || len(tools[0].Description) != stdioLargeResponseLen {
		t.Fatalf("ListTools() returned incomplete response: tools=%d description=%d", len(tools), toolDescriptionLength(tools))
	}
	if closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if repeatedCloseErr != closeErr {
		t.Fatalf("repeated Close() error = %v, want %v", repeatedCloseErr, closeErr)
	}
	if command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatalf("process state = %#v, want reaped exit", command.ProcessState)
	}
}

func TestCommandIOTransportStartFailureClosesOwnedPipeEnds(t *testing.T) {
	// Given
	command := exec.Command(filepath.Join(t.TempDir(), "missing-command"))
	transport := newCommandIOTransport(command, defaultStdioCloseTimeout)

	// When
	_, connectErr := transport.Connect(context.Background())
	firstCloseErr := transport.Close()
	secondCloseErr := transport.Close()

	// Then
	if connectErr == nil {
		t.Fatal("Connect() error = nil, want start failure")
	}
	if firstCloseErr != nil || secondCloseErr != firstCloseErr {
		t.Fatalf("Close() errors = (%v, %v), want stable nil", firstCloseErr, secondCloseErr)
	}
	stdoutWrite, stdoutOK := command.Stdout.(*os.File)
	stdinRead, stdinOK := command.Stdin.(*os.File)
	if !stdoutOK || !stdinOK {
		t.Fatalf("command pipes = (%T, %T), want *os.File", command.Stdout, command.Stdin)
	}
	if _, err := stdoutWrite.Write([]byte("closed")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("stdout child-side write error = %v, want os.ErrClosed", err)
	}
	if _, err := stdinRead.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("stdin child-side read error = %v, want os.ErrClosed", err)
	}
}

func TestDialStdioRejectsMultilineJSONFrame(t *testing.T) {
	// Given
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	client, err := DialStdio(context.Background(), StdioConfig{
		Command: executable,
		Env:     []string{stdioHelperModeEnv + "=pretty-init"},
	})
	if err != nil {
		t.Fatalf("DialStdio() error = %v", err)
	}

	// When
	_, initializeErr := client.Initialize(context.Background(), "stdio-client", "v1.0.0")
	closeErr := client.Close()

	// Then
	assertInvalidFrameError(t, initializeErr)
	if closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
}

func toolDescriptionLength(tools []message.ToolDefinition) int {
	if len(tools) == 0 {
		return 0
	}
	return len(tools[0].Description)
}

type stdioHandshake struct {
	ProtocolVersion  string   `json:"protocolVersion"`
	Capabilities     int      `json:"capabilities"`
	WorkingDirectory string   `json:"workingDirectory"`
	ParentSecret     string   `json:"parentSecret"`
	Arguments        []string `json:"arguments"`
}

type stdioWireObservation struct {
	FirstMethod      string `json:"firstMethod"`
	Capabilities     int    `json:"capabilities"`
	HadContentLength bool   `json:"hadContentLength"`
}
