//go:build icloud

package icloudadapter

import (
	"example.com/apple-notes-mcp/internal/model"
	"testing"
)

func TestScopeFolders(t *testing.T) {
	all := []model.Folder{
		{ID: "root", Name: "allowed"},
		{ID: "child", ParentID: "root", Name: "allowed-child"},
		{ID: "else", Name: "private"},
		{ID: "secret", ParentID: "else", Name: "private-child"},
	}
	allowed, vis, err := ScopeFolders(all, []string{"root"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vis) != 2 || !allowed["child"] || allowed["secret"] {
		t.Fatalf("failed closed? %+v %+v", allowed, vis)
	}
	if _, _, err := ScopeFolders(all, []string{"missing"}); err == nil {
		t.Fatal("missing whitelist root accepted")
	}
}
