package api

import (
	"context"
	"io"
	"testing"
)

type stubArtifactStore struct{}

func (stubArtifactStore) Put(context.Context, Artifact, io.Reader) (Artifact, error) {
	return Artifact{}, nil
}

func (stubArtifactStore) Get(context.Context, string) (io.ReadCloser, Artifact, error) {
	return nil, Artifact{}, nil
}

func (stubArtifactStore) Describe(context.Context, string) (Artifact, error) {
	return Artifact{}, nil
}

func (stubArtifactStore) List(context.Context, ArtifactSelector) ([]Artifact, error) {
	return nil, nil
}

func TestArtifactStoreIsHostImplementedContract(t *testing.T) {
	var store ArtifactStore = stubArtifactStore{}
	artifacts, err := store.List(context.Background(), ArtifactSelector{})
	if err != nil || artifacts != nil {
		t.Fatalf("ArtifactStore.List() = %#v, %v", artifacts, err)
	}
}
