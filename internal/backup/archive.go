package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// listFiles restituisce i file regolari sotto root, relativi e con '/', saltando le
// cartelle nascoste. root assente = nessun file.
func listFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && p == root {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() && p != root && d.Name()[0] == '.' {
			return filepath.SkipDir // es. .tmp: caricamenti a pezzi in corso
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}

func writeArchive(path string, m Manifest, dbFile, uploadRoot string, files []string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	err = func() error {
		mj, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: manifestEntry, Mode: 0o640, Size: int64(len(mj)),
			ModTime: m.CreatedAt, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(mj); err != nil {
			return err
		}
		if err := addFile(tw, dbEntry, dbFile); err != nil {
			return err
		}
		for _, rel := range files {
			err := addFile(tw, uploadsEntry+"/"+rel, filepath.Join(uploadRoot, filepath.FromSlash(rel)))
			if err != nil && !os.IsNotExist(err) { // file rimosso nel frattempo: lo salta
				return err
			}
		}
		if err := tw.Close(); err != nil {
			return err
		}
		return gz.Close()
	}()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func addFile(tw *tar.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: fi.Size(),
		ModTime: fi.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, fi.Size())
	return err
}
