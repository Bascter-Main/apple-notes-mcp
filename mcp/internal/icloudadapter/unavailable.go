//go:build !icloud

package icloudadapter

import (
	"context"
	"errors"

	"example.com/apple-notes-mcp/internal/model"
	"example.com/apple-notes-mcp/internal/security"
)

type Source struct{}

func NewSource(_ security.Store, _ []string, _ int) (*Source, error) {
	return nil, errors.New("build using: go build -tags icloud")
}
func (*Source) Fetch(context.Context) (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("iCloud build tag required")
}
