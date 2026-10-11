package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const aesKeyHex = "d27e154628ae2ba6ab4b9775165ff737"
const maxMLWSize int64 = 512 << 20

func decryptMLW(src string) (string, error) { return decryptFile(context.Background(), src) }

func decryptFile(ctx context.Context, src string) (string, error) {
	if !strings.EqualFold(filepath.Ext(src), ".mlw") {
		return "", errors.New("select an .mlw file")
	}
	f, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxMLWSize {
		return "", errors.New("select a regular MLW file smaller than 512 MiB")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxMLWSize+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxMLWSize {
		return "", errors.New("file exceeds the 512 MiB limit")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	plain, err := decodeMLW(data)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(src, filepath.Ext(src))
	return writeUnique(ctx, base, ".mp4", plain)
}

func decodeMLW(data []byte) ([]byte, error) {
	root := bytes.Index(data, []byte("Root\x00"))
	if root < 0 {
		return nil, errors.New("invalid MLW header: root marker missing")
	}
	start := root + 5
	end := bytes.IndexByte(data[start:], 0)
	if end < 0 {
		return nil, errors.New("invalid MLW header: filename is incomplete")
	}
	offset := start + end + 13
	if offset > len(data)-33 {
		return nil, errors.New("MLW payload is missing or truncated")
	}
	key, err := hex.DecodeString(aesKeyHex)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Decrypt in place to bound peak memory. Never release unauthenticated plaintext.
	ct := data[offset+16:]
	plain, err := gcm.Open(ct[:0], data[offset:offset+12], ct, nil)
	if err != nil {
		return nil, errors.New("authentication failed: this MLW file is damaged or unsupported")
	}
	return plain, nil
}

// Publish a complete file without replacing any existing destination, even if
// another converter claims the same name concurrently.
func writeUnique(ctx context.Context, base, ext string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(base), ".mlw-output-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	for i := 0; i < 10000; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		out := base + ext
		if i > 0 {
			out = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		if err := moveFile(name, out, false); err == nil {
			return out, nil
		} else if !isFileExists(err) {
			return "", fmt.Errorf("save output: %w", err)
		}
	}
	return "", errors.New("too many files with the same name")
}
