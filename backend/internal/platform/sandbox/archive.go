package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

// ErrArchive: the upload is not a script sandboxd can run; the text says why.
var ErrArchive = errors.New("sandbox: archive")

func archiveErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrArchive, fmt.Sprintf(format, a...))
}

var (
	entryRe   = regexp.MustCompile(`^[A-Za-z0-9._/@+-]{1,200}$`)
	projectRe = regexp.MustCompile(`^[A-Za-z0-9 ._:@/-]{1,100}$`)
)

// Summary is what CheckArchive found.
type Summary struct {
	Files int
	Bytes int64
	Entry string // the entry it resolved (the Playwright config it found)
}

// CleanEntry checks an entry path: relative, no "..", no odd characters.
func CleanEntry(entry string) (string, error) {
	entry = strings.TrimPrefix(strings.TrimSpace(entry), "./")
	if entry == "" {
		return "", nil
	}
	clean := path.Clean(entry)
	if !entryRe.MatchString(entry) || clean != entry || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", archiveErr("entry %q: a relative path inside the archive", entry)
	}
	return clean, nil
}

// CleanFilter checks a Playwright --project or --grep value; it reaches the
// container as an environment variable, never through a shell line.
func CleanFilter(name, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if !projectRe.MatchString(v) {
		return "", archiveErr("%s: letters, digits, spaces and . _ : @ / - (at most 100)", name)
	}
	return v, nil
}

// CheckArchive reads a .tar.gz before anything runs it: it must unpack,
// stay within the limits, hold only regular files and directories at
// relative paths, and contain the entry (k6: required; Playwright: the
// config named, or a playwright.config.* at the top). It does not unpack
// anything to disk; the container does, inside the sandbox.
func CheckArchive(archive []byte, kind, entry string) (Summary, error) {
	if kind != KindPlaywright && kind != KindK6 {
		return Summary{}, archiveErr(`kind is "playwright" or "k6"`)
	}
	if len(archive) == 0 {
		return Summary{}, archiveErr("empty upload")
	}
	if len(archive) > MaxArchiveBytes {
		return Summary{}, archiveErr("the archive is over %d MB", MaxArchiveBytes>>20)
	}
	entry, err := CleanEntry(entry)
	if err != nil {
		return Summary{}, err
	}
	if kind == KindK6 && entry == "" {
		return Summary{}, archiveErr("entry: the k6 script to run, e.g. load.js")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return Summary{}, archiveErr("not a .tar.gz (%v)", err)
	}
	// The tar stream itself is bounded too, headers included: entries
	// without content (folders, a name repeated, pax records) cost no
	// unpacked bytes, and a few MB of them compress to GBs to read.
	stream := &capReader{r: gz, left: maxStream}
	tr := tar.NewReader(stream)
	var s Summary
	entries := 0
	found := map[string]bool{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if stream.over {
			return Summary{}, archiveErr("over %d MB unpacked", MaxUnpacked>>20)
		}
		if err != nil {
			return Summary{}, archiveErr("the archive is broken (%v)", err)
		}
		if entries++; entries > MaxEntries {
			return Summary{}, archiveErr("more than %d files and folders", MaxEntries)
		}
		name := strings.TrimPrefix(h.Name, "./")
		if name == "" || name == "." {
			continue
		}
		clean := path.Clean(name)
		if strings.HasPrefix(name, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(name, "\\") {
			return Summary{}, archiveErr("%q: every path stays inside the archive", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		case tar.TypeXGlobalHeader:
			continue
		default:
			return Summary{}, archiveErr("%q: only files and folders (no links or devices)", h.Name)
		}
		s.Files++
		if s.Files > MaxFiles {
			return Summary{}, archiveErr("more than %d files", MaxFiles)
		}
		n, err := io.Copy(io.Discard, io.LimitReader(tr, MaxUnpacked-s.Bytes+1))
		if stream.over {
			return Summary{}, archiveErr("over %d MB unpacked", MaxUnpacked>>20)
		}
		if err != nil {
			return Summary{}, archiveErr("the archive is broken (%v)", err)
		}
		s.Bytes += n
		if s.Bytes > MaxUnpacked {
			return Summary{}, archiveErr("over %d MB unpacked", MaxUnpacked>>20)
		}
		found[clean] = true
	}
	switch {
	case entry != "":
		if !found[entry] {
			return Summary{}, archiveErr("%s is not in the archive", entry)
		}
		s.Entry = entry
	default: // Playwright, its default config
		for _, c := range []string{"playwright.config.ts", "playwright.config.js", "playwright.config.mjs", "playwright.config.mts", "playwright.config.cjs", "playwright.config.cts"} {
			if found[c] {
				s.Entry = c
				break
			}
		}
		if s.Entry == "" {
			return Summary{}, archiveErr("no playwright.config.* at the top of the archive: pack the project folder itself, or name the config in entry")
		}
	}
	return s, nil
}

// maxStream bounds the decompressed tar stream: the files' bytes plus room
// for every entry's headers (a long name or pax record is a few KB).
const maxStream = MaxUnpacked + MaxEntries*8<<10

// capReader reads at most left bytes, then fails and says so (over).
type capReader struct {
	r    io.Reader
	left int64
	over bool
}

var errStreamCap = errors.New("sandbox: tar stream over its limit")

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		c.over = true
		return 0, errStreamCap
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}
