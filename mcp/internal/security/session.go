package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

var magic = []byte("INOTES1")

// Store persists encrypted iCloud Session blobs. It does not persist note bodies.
// The key file and ciphertext file are separate but are protected by the same
// operating-system account. Encryption is NOT a substitute for host security.
type Store interface {
	Load() ([]byte, error)
	Save([]byte) error
}

type FileStore struct{ Dir string }

func (s FileStore) keyPath() string  { return filepath.Join(s.Dir, "session.key") }
func (s FileStore) dataPath() string { return filepath.Join(s.Dir, "session.enc") }

func checkPrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("session file must be regular (no symlink)")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("unsafe permissions for %s (need 0600)", path)
	}
	return nil
}

func (s FileStore) InitKey() error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(s.keyPath()); err == nil {
		return checkPrivateFile(s.keyPath())
	} else if !os.IsNotExist(err) {
		return err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return err
	}
	f, err := os.OpenFile(s.keyPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, werr := f.Write(key)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func (s FileStore) key() ([]byte, error) {
	if err := checkPrivateFile(s.keyPath()); err != nil {
		return nil, err
	}
	k, err := os.ReadFile(s.keyPath())
	if err != nil {
		return nil, err
	}
	if len(k) != 32 {
		return nil, errors.New("bad AES key length")
	}
	return k, nil
}

func Encrypt(k, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := append([]byte{}, magic...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plain, nil), nil
}
func Decrypt(k, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < len(magic)+gcm.NonceSize()+gcm.Overhead() || string(blob[:len(magic)]) != string(magic) {
		return nil, errors.New("invalid encrypted session format")
	}
	nonce := blob[len(magic) : len(magic)+gcm.NonceSize()]
	return gcm.Open(nil, nonce, blob[len(magic)+gcm.NonceSize():], nil)
}

func (s FileStore) Load() ([]byte, error) {
	k, err := s.key()
	if err != nil {
		return nil, err
	}
	if err := checkPrivateFile(s.dataPath()); err != nil {
		return nil, err
	}
	blob, err := os.ReadFile(s.dataPath())
	if err != nil {
		return nil, err
	}
	return Decrypt(k, blob)
}
func (s FileStore) Save(data []byte) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	k, err := s.key()
	if err != nil {
		return err
	}
	ct, err := Encrypt(k, data)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".session-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(ct); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.dataPath())
}

// StaticEnvStore is for future stateless SCF deployments. It deliberately cannot
// persist refreshed tokens. Reauthentication / redeployment may be necessary.
type StaticEnvStore struct{ KeyB64, CiphertextB64 string }

func (s StaticEnvStore) Load() ([]byte, error) {
	k, err := base64.StdEncoding.DecodeString(s.KeyB64)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(s.CiphertextB64)
	if err != nil {
		return nil, err
	}
	return Decrypt(k, b)
}
func (s StaticEnvStore) Save([]byte) error { return nil }
