package program

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryLoader_Load(t *testing.T) {
	tests := []struct {
		name    string
		loader  MemoryLoader
		docName string
		want    Document
		wantErr bool
	}{
		{
			name: "existing document",
			loader: MemoryLoader{
				Documents: map[string]Document{
					"test": {Name: "test", Body: "test body"},
				},
			},
			docName: "test",
			want:    Document{Name: "test", Body: "test body"},
			wantErr: false,
		},
		{
			name:    "empty loader",
			loader:  MemoryLoader{},
			docName: "test",
			want:    Document{},
			wantErr: true,
		},
		{
			name: "non-existing document",
			loader: MemoryLoader{
				Documents: map[string]Document{
					"other": {Name: "other", Body: "other body"},
				},
			},
			docName: "test",
			want:    Document{},
			wantErr: true,
		},
		{
			name: "document with metadata",
			loader: MemoryLoader{
				Documents: map[string]Document{
					"test": {
						Name:     "test",
						Body:     "test body",
						Metadata: map[string]string{"key": "value"},
					},
				},
			},
			docName: "test",
			want: Document{
				Name:     "test",
				Body:     "test body",
				Metadata: map[string]string{"key": "value"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := tt.loader.Load(ctx, tt.docName)

			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got.Name != tt.want.Name {
				t.Errorf("Load() Name = %v, want %v", got.Name, tt.want.Name)
			}
			if got.Body != tt.want.Body {
				t.Errorf("Load() Body = %v, want %v", got.Body, tt.want.Body)
			}
		})
	}
}

func TestMemoryLoader_Load_ErrorCase(t *testing.T) {
	loader := MemoryLoader{
		Documents: map[string]Document{
			"exists": {Name: "exists", Body: "body"},
		},
	}

	ctx := context.Background()
	_, err := loader.Load(ctx, "missing")

	if err != ErrProgramNotFound {
		t.Errorf("Load() error = %v, want ErrProgramNotFound", err)
	}
}

func TestFSLoader_Load(t *testing.T) {
	// Create temporary directory
	tempDir := t.TempDir()

	// Create test files
	testContent := "test program content"
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create file in subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}
	subFile := filepath.Join(subDir, "sub.txt")
	if err := os.WriteFile(subFile, []byte("sub content"), 0644); err != nil {
		t.Fatalf("Failed to create sub file: %v", err)
	}

	loader := FSLoader{Root: tempDir}

	tests := []struct {
		name    string
		docName string
		want    string
		wantErr bool
	}{
		{
			name:    "existing file",
			docName: "test.txt",
			want:    "test",
			wantErr: false,
		},
		{
			name:    "file without extension",
			docName: "test",
			want:    "test",
			wantErr: true, // file without extension won't be found
		},
		{
			name:    "non-existing file",
			docName: "missing.txt",
			want:    "",
			wantErr: true,
		},
		{
			name:    "file in subdirectory",
			docName: filepath.Join("subdir", "sub.txt"),
			want:    "sub",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := loader.Load(ctx, tt.docName)

			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got.Name != tt.want {
				t.Errorf("Load() Name = %v, want %v", got.Name, tt.want)
			}
		})
	}
}

func TestFSLoader_Load_Content(t *testing.T) {
	tempDir := t.TempDir()
	testContent := "hello world program"
	testFile := filepath.Join(tempDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	loader := FSLoader{Root: tempDir}
	ctx := context.Background()
	doc, err := loader.Load(ctx, "hello.txt")

	if err != nil {
		t.Errorf("Load() error = %v", err)
	}
	if doc.Body != testContent {
		t.Errorf("Load() Body = %v, want %v", doc.Body, testContent)
	}
	if doc.Name != "hello" {
		t.Errorf("Load() Name = %v, want hello", doc.Name)
	}
}

func TestDocument_Struct(t *testing.T) {
	doc := Document{
		Name:     "test-doc",
		Body:     "test body content",
		Metadata: map[string]string{"author": "tester", "version": "1.0"},
	}

	if doc.Name != "test-doc" {
		t.Errorf("Name = %v, want test-doc", doc.Name)
	}
	if doc.Body != "test body content" {
		t.Errorf("Body = %v, want test body content", doc.Body)
	}
	if len(doc.Metadata) != 2 {
		t.Errorf("len(Metadata) = %v, want 2", len(doc.Metadata))
	}
}

func TestErrProgramNotFound(t *testing.T) {
	if ErrProgramNotFound == nil {
		t.Error("ErrProgramNotFound should not be nil")
	}

	// Test that it's the expected error
	if ErrProgramNotFound.Error() != "program not found" {
		t.Errorf("ErrProgramNotFound.Error() = %v, want 'program not found'", ErrProgramNotFound.Error())
	}
}
