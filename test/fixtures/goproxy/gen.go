package goproxy

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	version = "v1.0.0"
	// modTime is fixed so the generated zips are byte-for-byte reproducible.
	modTime = "2000-01-01T00:00:00Z"
	prefix  = "example.invalid/cowflow/dep"
)

// Fixture is a generated proxy and its consumer module.
type Fixture struct {
	ProxyDir    string   // GOPROXY root (file layout)
	ConsumerDir string   // module that imports every synthetic module
	Modules     []string // module paths, all at v1.0.0
}

// Generate writes n synthetic modules into <dir>/proxy and a consumer
// module that imports them all into <dir>/consumer. dir must be missing or
// empty.
func Generate(dir string, n int) (Fixture, error) {
	if n <= 0 {
		return Fixture{}, errors.New("goproxy: need at least one module")
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return Fixture{}, fmt.Errorf("goproxy: %s is not empty", dir)
	}
	f := Fixture{ProxyDir: filepath.Join(dir, "proxy"), ConsumerDir: filepath.Join(dir, "consumer")}
	stamp, err := time.Parse(time.RFC3339, modTime)
	if err != nil {
		return Fixture{}, fmt.Errorf("goproxy: parse mod time: %w", err)
	}

	var imports, requires, uses strings.Builder
	for i := range n {
		mod := fmt.Sprintf("%s%04d", prefix, i)
		pkg := fmt.Sprintf("dep%04d", i)
		f.Modules = append(f.Modules, mod)
		if err := writeModule(f.ProxyDir, mod, pkg, i, stamp); err != nil {
			return Fixture{}, err
		}
		fmt.Fprintf(&imports, "\t%s \"%s\"\n", pkg, mod)
		fmt.Fprintf(&requires, "\t%s %s\n", mod, version)
		fmt.Fprintf(&uses, "\ttotal += %s.Value()\n", pkg)
	}

	gomod := "module example.invalid/cowflow/consumer\n\ngo 1.22\n\nrequire (\n" + requires.String() + ")\n"
	mainGo := "package main\n\nimport (\n\t\"fmt\"\n\n" + imports.String() + ")\n\n" +
		"func main() {\n\ttotal := 0\n" + uses.String() + "\tfmt.Println(total)\n}\n"
	if err := os.MkdirAll(f.ConsumerDir, 0o750); err != nil {
		return Fixture{}, fmt.Errorf("goproxy: create consumer: %w", err)
	}
	for name, content := range map[string]string{"go.mod": gomod, "main.go": mainGo} {
		if err := os.WriteFile(filepath.Join(f.ConsumerDir, name), []byte(content), 0o600); err != nil {
			return Fixture{}, fmt.Errorf("goproxy: write consumer %s: %w", name, err)
		}
	}
	return f, nil
}

// Env returns the environment that makes the go command use only this proxy:
// no network, no checksum database, no toolchain download.
func (f Fixture) Env() []string {
	return []string{
		"GOPROXY=" + fileURL(f.ProxyDir),
		"GOSUMDB=off",
		"GOFLAGS=-mod=mod",
		"GOTOOLCHAIN=local",
		"GOWORK=off",
		"GO111MODULE=on",
		// Belt and braces: anything that still tries HTTP fails at once.
		"HTTP_PROXY=http://127.0.0.1:9",
		"HTTPS_PROXY=http://127.0.0.1:9",
	}
}

// fileURL turns an absolute directory into a file:// URL the go command
// accepts on every OS (file:///C:/dir on Windows, file:///dir elsewhere).
func fileURL(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	s := filepath.ToSlash(abs)
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return "file://" + s
}

func writeModule(proxyDir, mod, pkg string, i int, stamp time.Time) error {
	vdir := filepath.Join(proxyDir, filepath.FromSlash(mod), "@v")
	if err := os.MkdirAll(vdir, 0o750); err != nil {
		return fmt.Errorf("goproxy: create %s: %w", vdir, err)
	}
	gomod := "module " + mod + "\n\ngo 1.22\n"
	src := fmt.Sprintf("package %s\n\n// Value is a constant of the synthetic module.\nfunc Value() int { return %d }\n", pkg, i+1)
	zipped, err := zipModule(mod, stamp, map[string]string{"go.mod": gomod, pkg + ".go": src})
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"list":            []byte(version + "\n"),
		version + ".info": []byte(fmt.Sprintf("{\"Version\":%q,\"Time\":%q}\n", version, modTime)),
		version + ".mod":  []byte(gomod),
		version + ".zip":  zipped,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(vdir, name), data, 0o600); err != nil {
			return fmt.Errorf("goproxy: write %s: %w", name, err)
		}
	}
	return nil
}

// zipModule builds a module zip: every file lives under <module>@<version>/.
func zipModule(mod string, stamp time.Time, files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names) // reproducible entry order
	for _, name := range names {
		if err := addFile(zw, mod, name, files[name], stamp); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("goproxy: close zip: %w", err)
	}
	return buf.Bytes(), nil
}

func addFile(zw *zip.Writer, mod, name, content string, stamp time.Time) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: mod + "@" + version + "/" + name, Method: zip.Deflate, Modified: stamp})
	if err != nil {
		return fmt.Errorf("goproxy: zip %s: %w", name, err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return fmt.Errorf("goproxy: zip %s: %w", name, err)
	}
	return nil
}
