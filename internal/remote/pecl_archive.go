package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/hightemp/phvm/internal/fsutil"
)

type manifestFile struct {
	Name string `xml:"name,attr"`
	MD5  string `xml:"md5sum,attr"`
}
type manifestDir struct {
	Name  string         `xml:"name,attr"`
	Dirs  []manifestDir  `xml:"dir"`
	Files []manifestFile `xml:"file"`
}
type packageManifest struct {
	XMLName       xml.Name       `xml:"package"`
	Name          string         `xml:"name"`
	Channel       string         `xml:"channel"`
	Module        string         `xml:"providesextension"`
	Version       string         `xml:"version>release"`
	LegacyVersion string         `xml:"release>version"`
	Dirs          []manifestDir  `xml:"contents>dir"`
	LegacyFiles   []manifestFile `xml:"release>filelist>file"`
}

// PECLManifest describes independently fetched channel metadata, not a signature.
type PECLManifest struct {
	Name, Version, Module string
	Files                 map[string]string
}

func decodePackage(data []byte, name, version string) (*packageManifest, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if !strings.EqualFold(charset, "ISO-8859-1") {
			return nil, fmt.Errorf("unsupported PECL XML charset %q", charset)
		}
		data, err := io.ReadAll(input)
		if err != nil {
			return nil, err
		}
		var out strings.Builder
		for _, b := range data {
			out.WriteRune(rune(b))
		}
		return strings.NewReader(out.String()), nil
	}
	var manifest packageManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse PECL manifest: %w", err)
	}
	if manifest.Version == "" {
		manifest.Version = manifest.LegacyVersion
	}
	if !strings.EqualFold(manifest.Name, name) || version != "" && manifest.Version != version || manifest.Channel != "" && manifest.Channel != "pecl.php.net" {
		return nil, fmt.Errorf("PECL manifest identity mismatch")
	}
	return &manifest, nil
}

// PECLPackageModule reads the provided module, including legacy channel encodings.
func PECLPackageModule(data []byte, name string) (string, error) {
	manifest, err := decodePackage(data, name, "")
	if err != nil {
		return "", err
	}
	return manifest.Module, nil
}

func safeArchivePath(name string) error {
	if name == "" || path.IsAbs(name) || path.Clean(name) != name || strings.Contains(name, "\\") {
		return fmt.Errorf("unsafe PECL archive path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if err := fsutil.ValidateName(part); err != nil {
			return err
		}
	}
	return nil
}

// GetManifest fetches file checksums separately from the executable archive.
func (api *PECLAPI) GetManifest(ctx context.Context, name, version string) (*PECLManifest, error) {
	if err := fsutil.ValidateName(name); err != nil {
		return nil, err
	}
	if err := fsutil.ValidateName(version); err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/rest/r/%s/package.%s.xml", api.baseURL, strings.ToLower(name), version)
	response, err := api.client.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("PECL manifest HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 8*1024*1024 {
		return nil, fmt.Errorf("PECL manifest exceeds size limit")
	}
	parsed, err := decodePackage(data, name, version)
	if err != nil {
		return nil, err
	}
	manifest := &PECLManifest{Name: name, Version: version, Module: parsed.Module, Files: make(map[string]string)}
	add := func(prefix string, file manifestFile) error {
		name := file.Name
		if prefix != "" {
			name = prefix + "/" + name
		}
		if err := safeArchivePath(name); err != nil {
			return err
		}
		hash := strings.ToLower(file.MD5)
		if len(hash) != 32 {
			return fmt.Errorf("PECL file checksum missing or invalid: %s", name)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return err
		}
		if _, ok := manifest.Files[name]; ok {
			return fmt.Errorf("duplicate PECL manifest file: %s", name)
		}
		manifest.Files[name] = hash
		return nil
	}
	var collect func(manifestDir, string, int) error
	collect = func(dir manifestDir, prefix string, depth int) error {
		if depth > 64 {
			return fmt.Errorf("PECL manifest nesting exceeds limit")
		}
		if prefix != "" || dir.Name != "/" {
			if err := safeArchivePath(dir.Name); err != nil {
				return err
			}
			if prefix == "" {
				prefix = dir.Name
			} else {
				prefix += "/" + dir.Name
			}
		}
		for _, file := range dir.Files {
			if err := add(prefix, file); err != nil {
				return err
			}
		}
		for _, child := range dir.Dirs {
			if err := collect(child, prefix, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, dir := range parsed.Dirs {
		if err := collect(dir, "", 0); err != nil {
			return nil, err
		}
	}
	for _, file := range parsed.LegacyFiles {
		if err := add("", file); err != nil {
			return nil, err
		}
	}
	if len(manifest.Files) == 0 {
		return nil, fmt.Errorf("PECL manifest has no checked files")
	}
	return manifest, nil
}

type contextReader struct {
	context.Context
	io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// ValidatePECLArchive checks structure and identity before extraction or execution.
// Channel MD5 values detect archive/metadata disagreement; SHA256 pinning is separate.
func ValidatePECLArchive(ctx context.Context, filename, name, version string, manifest *PECLManifest) error {
	root, err := os.OpenRoot(filepath.Dir(filename))
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(filepath.Base(filename))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("PECL archive must be a regular file")
	}
	file, err := root.Open(filepath.Base(filename))
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := &io.LimitedReader{R: contextReader{ctx, gz}, N: 1024*1024*1024 + 1}
	tr := tar.NewReader(reader)
	seen := make(map[string]bool)
	checked := make(map[string]bool)
	hasManifest := false
	prefix := strings.ToLower(name+"-"+version) + "/"
	var total int64
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		entry := strings.TrimSuffix(strings.TrimPrefix(header.Name, "./"), "/")
		if err := safeArchivePath(entry); err != nil {
			return err
		}
		if seen[entry] {
			return fmt.Errorf("duplicate PECL archive entry: %s", entry)
		}
		seen[entry] = true
		if len(seen) > 100000 || header.Size < 0 || header.Size > 1024*1024*1024-total {
			return fmt.Errorf("PECL archive exceeds limits")
		}
		total += header.Size
		if header.Typeflag == tar.TypeDir {
			if strings.ToLower(entry) != strings.TrimSuffix(prefix, "/") && !strings.HasPrefix(strings.ToLower(entry), prefix) {
				return fmt.Errorf("unexpected PECL archive directory")
			}
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("PECL archive contains a link or special file")
		}
		if entry == "package.xml" || entry == "package2.xml" {
			if header.Size > 8*1024*1024 {
				return fmt.Errorf("PECL package.xml exceeds limit")
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			parsed, err := decodePackage(data, name, version)
			if err != nil {
				return err
			}
			if manifest != nil && !strings.EqualFold(parsed.Module, manifest.Module) {
				return fmt.Errorf("PECL provided module mismatch")
			}
			if entry == "package.xml" {
				hasManifest = true
			}
			continue
		}
		if !strings.HasPrefix(strings.ToLower(entry), prefix) {
			return fmt.Errorf("unexpected PECL archive file %s", entry)
		}
		key := entry[len(prefix):]
		if manifest != nil {
			expected, ok := manifest.Files[key]
			if !ok {
				return fmt.Errorf("PECL file is absent from channel manifest: %s", key)
			}
			hash := md5.New()
			if _, err := io.CopyN(hash, tr, header.Size); err != nil {
				return err
			}
			if hex.EncodeToString(hash.Sum(nil)) != expected {
				return fmt.Errorf("PECL file checksum mismatch: %s", key)
			}
			checked[key] = true
		}
	}
	if !hasManifest {
		return fmt.Errorf("PECL package.xml missing")
	}
	if manifest != nil && len(checked) != len(manifest.Files) {
		return fmt.Errorf("PECL archive is missing channel manifest files")
	}
	_, err = io.Copy(io.Discard, reader)
	if reader.N == 0 {
		return fmt.Errorf("PECL archive exceeds decompression limit")
	}
	return err // Consume gzip trailer to check CRC/truncation.
}
