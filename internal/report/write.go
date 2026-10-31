package report

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteFiles writes report.json and report.md into dir, each via a temp
// file + rename so a crash mid-write never leaves a half-written report.
func WriteFiles(dir string, r Report) error {
	j, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(dir, "report.json"), j); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "report.md"), []byte(Markdown(r)))
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
