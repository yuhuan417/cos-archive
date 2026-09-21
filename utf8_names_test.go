package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// badName is a filename Linux accepts but JSON cannot hold verbatim: the exact
// bytes of the mojibake immich resource that broke the real archive.
// \xc8\xa1 is valid UTF-8 (ȡ) and \xe3\xb8\xa6 too (㸦); the \xa6, \xa9 and
// \xc0\xa6 around them are not.
const badName = "\xc8\xa1+\xa6\xe3\xb8\xa6\xa9\xc0\xa6"

func TestPathCodecRoundTrip(t *testing.T) {
	cases := []string{
		"/photo/正常的中文名.jpg",    // valid, must stay byte-identical
		"data/caf\xe9.txt",     // simple latin-1
		"/photo/" + badName,    // the real one
		"lit‛eral.txt",         // a name holding the quote rune itself
		"‛‛‛",                  // nothing but quote runes
		"tail\xff",             // bad byte at the very end
		"head\xffrest",         // bad byte in the middle
		"\xed\xa0\x80",         // a UTF-8 encoded surrogate, which Go rejects
		"",                     // empty
		"plain/ascii/path.txt", // nothing to do
		"per%cent%25.txt",      // percent signs are not special here
	}
	for _, raw := range cases {
		enc := encodePath(raw)
		if !utf8.ValidString(enc) {
			t.Fatalf("encoded form of %q is not valid UTF-8: %q", raw, enc)
		}
		if strings.ContainsRune(enc, 0) {
			t.Fatalf("encoded form of %q contains NUL: %q", raw, enc)
		}
		if got := decodePath(enc); got != raw {
			t.Fatalf("round trip of %q: encoded to %q, decoded to %q", raw, enc, got)
		}
		if utf8.ValidString(raw) && !strings.ContainsRune(raw, quoteRune) {
			if enc != raw {
				t.Fatalf("valid name %q was rewritten to %q", raw, enc)
			}
		}
	}
}

func TestEncodePathMatchesRcloneConvention(t *testing.T) {
	// rclone documents that the invalid byte 0xFE is stored as ‛FE.
	if got, want := encodePath("a\xfeb"), "a‛FEb"; got != want {
		t.Fatalf("encodePath = %q, want %q", got, want)
	}
	// and that the marker itself is doubled
	if got, want := encodePath("a‛b"), "a‛‛b"; got != want {
		t.Fatalf("encodePath = %q, want %q", got, want)
	}
}

func TestScanIndexesNonUTF8Names(t *testing.T) {
	baseDir := t.TempDir()
	goodFile := filepath.Join(baseDir, "good.txt")
	if err := os.WriteFile(goodFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	badFile := filepath.Join(baseDir, badName)
	if err := os.WriteFile(badFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	badDir := filepath.Join(baseDir, "dir\xe9")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(badDir, "inner.txt")
	if err := os.WriteFile(inner, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	badLink := filepath.Join(baseDir, "link")
	if err := os.Symlink("/target\xe9", badLink); err != nil {
		t.Fatal(err)
	}

	index := NewIndex(Config{BasePath: baseDir, FilePaths: []string{"/"}}, nil)
	index.GenerateLocal(NewIndex(Config{}, nil))

	for _, want := range []string{goodFile, badFile, inner} {
		if _, ok := index.Entries.Files[want]; !ok {
			t.Fatalf("expected %q in the index, got %#v", want, index.Entries.Files)
		}
	}
	if _, ok := index.Entries.Dirs[badDir]; !ok {
		t.Fatalf("expected %q in the index, got %#v", badDir, index.Entries.Dirs)
	}
	if got := index.Entries.Links[badLink]; got == nil {
		t.Fatalf("expected %q in the index, got %#v", badLink, index.Entries.Links)
	} else if got.LinkTo != Path("/target\xe9") {
		t.Fatalf("symlink target = %q, want %q", got.LinkTo, "/target\xe9")
	}
}

func TestIndexRoundTripsNonUTF8Names(t *testing.T) {
	index := NewIndex(Config{}, nil)
	hash := HashType{0xaa, 0xbb, 0xcc}
	files := []string{
		"/data/正常.jpg",
		"/data/" + badName,
		"/data/caf\xe9.txt",
		"/data/quo‛te.txt",
	}
	for _, f := range files {
		index.Entries.Files[f] = &FileInfo{Size: 12, Mode: 0644, ModTime: 123, Hash: hash}
	}
	index.Entries.Dirs["/data/d\xe9r"] = &DirInfo{Mode: 0755}
	index.Entries.Links["/data/link"] = &LinkInfo{LinkTo: Path("/target\xe9")}
	index.DeletedChunks[ChunkKey{size: 12, hash: hash}] = 456

	fp := filepath.Join(t.TempDir(), "meta.json")
	if err := index.Save(fp); err != nil {
		t.Fatalf("save with non-UTF-8 names: %v", err)
	}

	raw, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(raw) {
		t.Fatal("the index on disk is not valid UTF-8")
	}
	if bytes.ContainsRune(raw, 0) {
		t.Fatal("the index on disk contains NUL")
	}
	if !strings.Contains(string(raw), "‛") {
		t.Fatal("expected the escaped form to appear in the index")
	}

	loaded := NewIndex(Config{}, nil)
	if err := loaded.Load(fp); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(index.Entries, loaded.Entries) {
		t.Fatalf("entries mismatch:\n%#v\n%#v", index.Entries, loaded.Entries)
	}
	if !reflect.DeepEqual(index.DeletedChunks, loaded.DeletedChunks) {
		t.Fatalf("deleted chunks mismatch")
	}
}

func TestIndexEncodingIsByteStable(t *testing.T) {
	index := NewIndex(Config{}, nil)
	for _, f := range []string{"/b", "/a", "/data/" + badName, "/c\xe9"} {
		index.Entries.Files[f] = &FileInfo{Size: 1, Hash: HashType{0x01}}
	}

	dir := t.TempDir()
	first := filepath.Join(dir, "a.json")
	second := filepath.Join(dir, "b.json")
	if err := index.Save(first); err != nil {
		t.Fatal(err)
	}
	if err := index.Save(second); err != nil {
		t.Fatal(err)
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	// UploadRemote decides whether to re-upload by comparing sha1 of the
	// encoded index, so the bytes have to match run to run.
	if !bytes.Equal(a, b) {
		t.Fatalf("index bytes are not stable:\n%s\n%s", a, b)
	}
}

func TestSaveLeavesExistingIndexIntactWhenItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}
	dir := t.TempDir()
	fp := filepath.Join(dir, "meta.json")

	good := NewIndex(Config{}, nil)
	good.Entries.Files["/ok"] = &FileInfo{Size: 1}
	if err := good.Save(fp); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}

	// Save writes a temp file before touching the target, so a write that
	// cannot even start must leave the previous index exactly as it was.
	// This is what os.Create would have destroyed.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	next := NewIndex(Config{}, nil)
	next.Entries.Files["/other"] = &FileInfo{Size: 2}
	if err := next.Save(fp); err == nil {
		t.Fatal("expected Save to fail on a read-only directory")
	}

	after, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("existing index is gone: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the existing index changed")
	}
}

func TestLoadRejectsGenuinelyInvalidUTF8(t *testing.T) {
	// Hand-written junk that the encoder would never produce must still fail
	// loudly rather than be silently accepted.
	fp := filepath.Join(t.TempDir(), "meta.json")
	if err := os.WriteFile(fp, []byte(`{"Entries":{"Files":{"a`+"\xe9"+`.txt":{}}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	index := NewIndex(Config{}, nil)
	if err := index.Load(fp); err == nil {
		t.Fatal("expected the loader to reject raw invalid UTF-8")
	}
}

func TestDecodePathLeavesLoneQuoteRuneAlone(t *testing.T) {
	// encodePath never emits this, but a hand-edited index might.
	if got := decodePath("a‛b"); got != "a‛b" {
		t.Fatalf("decodePath = %q, want the rune left as-is", got)
	}
}

func TestNoSkipLoggingRemains(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, badName), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	var logBuf strings.Builder
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(&SimpleHandler{writer: &logBuf}))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	index := NewIndex(Config{BasePath: baseDir, FilePaths: []string{"/"}}, nil)
	index.GenerateLocal(NewIndex(Config{}, nil))

	if len(index.Entries.Files) != 1 {
		t.Fatalf("expected the file to be indexed, got %#v", index.Entries.Files)
	}
	if strings.Contains(logBuf.String(), "Skipping local entry") {
		t.Fatalf("nothing should be skipped any more, got:\n%s", logBuf.String())
	}
}
