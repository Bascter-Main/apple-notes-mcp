//go:build icloud

package icloudadapter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/torkve/icloud-notes/icloud"
	"github.com/torkve/icloud-notes/notes"

	"example.com/apple-notes-mcp/internal/model"
	"example.com/apple-notes-mcp/internal/security"
)

type Source struct {
	Store          security.Store
	AllowedRootIDs []string
	MaxScanNotes   int
	mu             sync.Mutex
}

func NewSource(store security.Store, roots []string, maxNotes int) (*Source, error) {
	rootSet := map[string]bool{}
	unique := []string{}
	for _, s := range roots {
		id := strings.TrimSpace(s)
		if id != "" && !rootSet[id] {
			unique = append(unique, id)
			rootSet[id] = true
		}
	}
	if len(unique) == 0 {
		return nil, errors.New("refusing to start: ALLOWED_FOLDER_IDS is empty")
	}
	if maxNotes <= 0 {
		maxNotes = 1000
	}
	return &Source{Store: store, AllowedRootIDs: unique, MaxScanNotes: maxNotes}, nil
}

func Open(ctx context.Context, store security.Store) (*icloud.Client, error) {
	blob, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load encrypted session: %w", err)
	}
	sess, err := icloud.ParseSession(blob)
	if err != nil {
		return nil, fmt.Errorf("invalid session: %w", err)
	}
	auth := icloud.NewAuthenticator(icloud.WithSRPVariant(1))
	cli, err := auth.Restore(ctx, sess)
	if err != nil {
		return nil, fmt.Errorf("session expired; run notesctl login to reauthenticate: %w", err)
	}
	return cli, nil
}

func SaveSession(cli *icloud.Client, store security.Store) error {
	data, err := cli.Session().MarshalBinary()
	if err != nil {
		return err
	}
	return store.Save(data)
}

// ScopeFolders is kept separate so its fail-closed behavior is independently
// tested, without network access or Apple credentials.
func ScopeFolders(all []model.Folder, roots []string) (map[string]bool, []model.Folder, error) {
	byID := map[string]model.Folder{}
	for _, f := range all {
		if f.ID != "" {
			byID[f.ID] = f
		}
	}
	allowed := map[string]bool{}
	for _, root := range roots {
		if _, ok := byID[root]; !ok {
			return nil, nil, fmt.Errorf("allowed folder %q not found in iCloud: stop rather than expanding scope", root)
		}
		allowed[root] = true
	}
	for i := 0; i <= len(byID); i++ {
		changed := false
		for _, f := range byID {
			if !allowed[f.ID] && allowed[f.ParentID] && f.ParentID != "" {
				allowed[f.ID] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	visible := make([]model.Folder, 0, len(allowed))
	for _, f := range byID {
		if allowed[f.ID] {
			visible = append(visible, f)
		}
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].ID < visible[j].ID })
	return allowed, visible, nil
}

func toTime(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	v := time.UnixMilli(ms).UTC()
	if v.Year() < 2000 || v.Year() > 2100 {
		return nil
	}
	return &v
}

func (s *Source) Fetch(ctx context.Context) (model.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cli, err := Open(ctx, s.Store)
	if err != nil {
		return model.Snapshot{}, err
	}
	folders, err := cli.ListFolders(ctx, notes.FolderID(""))
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("list iCloud folders: %w", err)
	}
	all := make([]model.Folder, 0, len(folders))
	for _, f := range folders {
		all = append(all, model.Folder{ID: string(f.ID), ParentID: string(f.ParentID), Name: f.Name})
	}
	allowed, visible, err := ScopeFolders(all, s.AllowedRootIDs)
	if err != nil {
		return model.Snapshot{}, err
	}
	// The upstream Changes method is scoped per root but may still include
	// records with an unresolved/empty folder ID. Apply our own scope check.
	byID := map[string]model.Note{}
	unresolved := 0
	for _, root := range s.AllowedRootIDs {
		cs, err := cli.Changes(ctx, notes.FolderID(root), "")
		if err != nil {
			return model.Snapshot{}, fmt.Errorf("read iCloud changes: %w", err)
		}
		for _, n := range cs.Notes {
			fid := string(n.FolderID)
			if fid == "" {
				unresolved++
				continue
			}
			if !allowed[fid] {
				continue
			}
			if _, exists := byID[string(n.ID)]; exists {
				continue
			}
			paragraphs := make([]string, 0, len(n.Body.Paragraphs))
			for _, p := range n.Body.Paragraphs {
				if p.Attachment != nil {
					continue
				} // no media bytes or remote URLs
				paragraphs = append(paragraphs, p.Text)
			}
			byID[string(n.ID)] = model.Note{
				ID: string(n.ID), FolderID: fid, Title: n.Title,
				CreatedAt: toTime(n.CreatedAtUnixMs), ModifiedAt: toTime(n.ModifiedAtUnixMs),
				Text: strings.Join(paragraphs, "\n"), Images: len(n.Images), BodyComplete: n.BodyDecoded,
			}
			if len(byID) > s.MaxScanNotes {
				return model.Snapshot{}, fmt.Errorf("allowed notes exceeded MAX_SCAN_NOTES (%d), refusing partial snapshot", s.MaxScanNotes)
			}
		}
	}
	// Save any automatically refreshed Apple session or CloudKit token changes.
	if err := SaveSession(cli, s.Store); err != nil {
		return model.Snapshot{}, fmt.Errorf("persist refreshed session: %w", err)
	}
	result := model.Snapshot{FetchedAt: time.Now().UTC(), Folders: visible, UnresolvedCount: unresolved}
	result.Notes = make([]model.Note, 0, len(byID))
	for _, n := range byID {
		result.Notes = append(result.Notes, n)
	}
	sort.Slice(result.Notes, func(i, j int) bool { return result.Notes[i].ID < result.Notes[j].ID })
	if unresolved > 0 {
		result.Warnings = append(result.Warnings, "部分笔记缺少可验证的文件夹 ID，已拒绝返回")
	}
	missing, partial := 0, 0
	for _, n := range result.Notes {
		if n.ModifiedAt == nil {
			missing++
		}
		if !n.BodyComplete {
			partial++
		}
	}
	if missing > 0 {
		result.Warnings = append(result.Warnings, "部分笔记缺少修改时间；不能可靠地将这些内容归入本周")
	}
	if partial > 0 {
		result.Warnings = append(result.Warnings, "部分笔记正文不能完整解码；请勿将其片段当作全文")
	}
	return result, nil
}
