package assets

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed defaults
var defaults embed.FS

// Init writes missing defaults individually; existing operator data wins.
func Init(dir string) error {
	return fs.WalkDir(defaults, "defaults", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel("defaults", path)
		if e != nil {
			return e
		}
		target := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0750)
		}
		f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
		if os.IsExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		b, e := defaults.ReadFile(path)
		if e == nil {
			_, e = f.Write(b)
		}
		ce := f.Close()
		if e != nil {
			os.Remove(target)
			return e
		}
		return ce
	})
}
