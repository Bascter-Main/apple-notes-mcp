package model

import (
	"context"
	"time"
)

// Note is a copy of fields permitted to leave the iCloud reader.
// All timestamps are provided by CloudKit in Unix milliseconds; a zero time
// means CloudKit did not supply a trustworthy value.
type Note struct {
	ID           string     `json:"id"`
	FolderID     string     `json:"folder_id"`
	Title        string     `json:"title"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	ModifiedAt   *time.Time `json:"modified_at,omitempty"`
	Text         string     `json:"text,omitempty"`
	Images       int        `json:"image_count"`
	BodyComplete bool       `json:"body_complete"`
}

type Folder struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Name     string `json:"name"`
}

type Snapshot struct {
	FetchedAt       time.Time `json:"fetched_at"`
	Folders         []Folder  `json:"folders"`
	Notes           []Note    `json:"notes"`
	UnresolvedCount int       `json:"unresolved_folder_count"`
	Warnings        []string  `json:"warnings,omitempty"`
}

// Source never writes notes. Persistent iCloud session state is handled only
// in the private reader, and is not exposed as an MCP tool.
type Source interface {
	Fetch(context.Context) (Snapshot, error)
}
