package security

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStore(t *testing.T) {
	s := FileStore{Dir: t.TempDir()}
	if err := s.InitKey(); err != nil {
		t.Fatal(err)
	}
	plain := []byte("confidential-test-session-token")
	if err := s.Save(plain); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("roundtrip failed")
	}
	disk, err := os.ReadFile(filepath.Join(s.Dir, "session.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, plain) {
		t.Fatal("plaintext leaked")
	}
}
func TestRejectWorldReadableKey(t *testing.T) {
	s := FileStore{Dir: t.TempDir()}
	if err := s.InitKey(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(s.Dir, "session.key"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err == nil {
		t.Fatal("weak key file permissions accepted")
	}
}
func TestTamperRejected(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	blob, err := Encrypt(key, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-1] ^= 1
	if _, err := Decrypt(key, blob); err == nil {
		t.Fatal("tampering must fail")
	}
}
