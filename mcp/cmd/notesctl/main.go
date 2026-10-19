//go:build icloud

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/torkve/icloud-notes/icloud"
	"github.com/torkve/icloud-notes/notes"

	"example.com/icloud-notes-mcp/internal/icloudadapter"
	"example.com/icloud-notes-mcp/internal/security"
)

func main() {
	action := flag.String("action", "login", "login | login-test | session-check | folders | probe")
	stateDir := flag.String("state-dir", "./.private", "private session directory")
	variant := flag.Int("srp-variant", 1, "SRP variant (upstream default example: 1)")
	root := flag.String("root", "", "folder id for probe; prints counts only")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	store := security.FileStore{Dir: *stateDir}
	var err error
	switch *action {
	case "login":
		err = login(ctx, store, *variant, false)
	case "login-test":
		err = login(ctx, store, *variant, true)
	case "session-check":
		err = sessionCheck(store)
	case "folders":
		err = list(ctx, store)
	case "probe":
		err = probe(ctx, store, *root)
	default:
		err = fmt.Errorf("invalid -action %q", *action)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", sanitizeErr(err))
		os.Exit(1)
	}
}
func prompt(reader *bufio.Reader, label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	text, err := reader.ReadString('\n')
	return strings.TrimSpace(text), err
}
func readSecret(reader *bufio.Reader) (string, error) {
	if os.Getenv("TERM") == "" {
		return "", fmt.Errorf("password input requires an interactive terminal")
	}
	off := exec.Command("stty", "-echo")
	off.Stdin = os.Stdin
	if err := off.Run(); err != nil {
		return "", fmt.Errorf("cannot disable terminal echo")
	}
	defer func() { on := exec.Command("stty", "echo"); on.Stdin = os.Stdin; _ = on.Run(); fmt.Fprintln(os.Stderr) }()
	return prompt(reader, "Apple ID password (hidden): ")
}
func login(ctx context.Context, store security.FileStore, variant int, liveTest bool) error {
	if err := store.InitKey(); err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	id, err := prompt(reader, "Apple ID email: ")
	if err != nil {
		return err
	}
	pw, err := readSecret(reader)
	if err != nil {
		return err
	}
	defer func() { pw = "" }()
	if id == "" || pw == "" {
		return fmt.Errorf("missing email or password")
	}
	fmt.Fprintln(os.Stderr, "Signing in to iCloud. A trusted-device prompt may appear...")
	auth := icloud.NewAuthenticator(icloud.WithSRPVariant(variant))
	result, err := auth.SignIn(ctx, id, pw)
	if err != nil {
		return fmt.Errorf("Apple sign-in failed: %w", err)
	}
	client := result.Client
	if result.Pending {
		code, err := prompt(reader, "Enter the Apple verification code: ")
		if err != nil {
			return err
		}
		if len(code) != 6 {
			return fmt.Errorf("expected a 6-digit Apple code")
		}
		client, err = auth.VerifyCode(ctx, result.Handle, code)
		if err != nil {
			return fmt.Errorf("2FA failed: %w", err)
		}
	}
	if client == nil {
		return fmt.Errorf("no signed-in client returned")
	}
	if liveTest {
		fmt.Fprintln(os.Stderr, "Testing iCloud Notes in the original login process. No note text will be printed...")
		folders, err := client.ListFolders(ctx, notes.FolderID(""))
		if err != nil {
			return fmt.Errorf("LIVE_READ_FAILED (before saving/restoring session): %w", err)
		}
		fmt.Fprintf(os.Stderr, "LIVE_READ_OK (%d folders; names withheld)\n", len(folders))
	}
	if err := icloudadapter.SaveSession(client, store); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Encrypted session stored locally (no note data written). Use -action folders to select a permitted folder.")
	return nil
}

// sessionCheck only inspects metadata of the locally decrypted session.
// It never makes an Apple request and deliberately prints no identifiers,
// URLs, tokens, cookie names, cookie values, or personal notes.
func sessionCheck(store security.Store) error {
	blob, err := store.Load()
	if err != nil {
		return fmt.Errorf("could not load encrypted session: %w", err)
	}
	sess, err := icloud.ParseSession(blob)
	if err != nil {
		return fmt.Errorf("could not parse saved session: %w", err)
	}
	fmt.Printf("session_metadata: apple_id_present=%t session_token_present=%t trust_token_present=%t dsid_present=%t client_id_present=%t database_url_present=%t cookies_count=%d expiry_present=%t expired_according_to_upstream=%t\n",
		sess.AppleID != "", sess.SessionToken != "", sess.TrustToken != "", sess.DSID != "", sess.ClientID != "", sess.DatabaseURL() != "", len(sess.Cookies), !sess.ExpiresAt.IsZero(), sess.Expired())
	fmt.Println("This checks session shape only; it does NOT prove CloudKit authorization.")
	return nil
}

func list(ctx context.Context, store security.Store) error {
	c, err := icloudadapter.Open(ctx, store)
	if err != nil {
		return err
	}
	fs, err := c.ListFolders(ctx, notes.FolderID(""))
	if err != nil {
		return err
	}
	for _, f := range fs {
		fmt.Printf("id=%q parent=%q name=%q\n", string(f.ID), string(f.ParentID), f.Name)
	}
	return icloudadapter.SaveSession(c, store)
}
func probe(ctx context.Context, store security.Store, root string) error {
	if root == "" {
		return fmt.Errorf("-root folder-id is required")
	}
	c, err := icloudadapter.Open(ctx, store)
	if err != nil {
		return err
	}
	cs, err := c.Changes(ctx, notes.FolderID(root), "")
	if err != nil {
		return err
	}
	fmt.Printf("Read succeeded: %d note records (contents withheld), %d descendant folders\n", len(cs.Notes), len(cs.Folders))
	return icloudadapter.SaveSession(c, store)
}
func sanitizeErr(err error) string {
	// Upstream Apple auth errors may contain HTTP response bodies. Do not echo
	// them to terminals that may be recorded by a remote shell service.
	msg := err.Error()
	if strings.Contains(msg, "Apple sign-in failed") || strings.Contains(msg, "2FA failed") {
		return "Apple authentication failed; check Apple ID, password, 2FA prompt, and upstream compatibility; avoid pasting sensitive logs"
	}
	return msg
}
