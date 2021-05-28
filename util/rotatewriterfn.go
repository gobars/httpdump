package util

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type RotateFileWriter struct {
	FnTemplate string
	MaxSize    uint64
	Append     bool

	file        *os.File
	currentFn   string
	currentSize uint64
	rotateFunc  func() bool
	writer      *bufio.Writer
}

func NewRotateFileWriter(filenameTemplate string, maxSize uint64, append bool) *RotateFileWriter {
	r := &RotateFileWriter{
		FnTemplate: filenameTemplate,
		MaxSize:    maxSize,
		Append:     append,
		rotateFunc: func() bool { return true },
	}

	if r.MaxSize > 0 {
		r.rotateFunc = func() bool {
			return r.currentSize >= r.MaxSize
		}
	}

	return r
}

func (w *RotateFileWriter) Write(p []byte) (int, error) {
	newFn := NewFilename(w.FnTemplate)

	for {
		fn, index := RotateFilename(newFn, w.rotateFunc())
		if fn == w.currentFn {
			break
		}

		if ok, err := w.openFile(fn, index); err != nil {
			return 0, err
		} else if ok {
			break
		}
	}

	n, err := w.writer.Write(p)
	w.currentSize += uint64(n)
	return n, err
}

func (w *RotateFileWriter) openFile(fn string, index int) (ok bool, err error) {
	_ = w.Close()
	if index == 2 { // rename bbb-2021-05-27-18-26.http to bbb-2021-05-27-18-26_00001.http
		_ = os.Rename(w.currentFn, SetFileIndex(w.currentFn, 1))
	}

	w.file, err = os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o660)
	if err != nil {
		return false, err
	}

	w.currentFn = fn
	w.writer = bufio.NewWriter(w.file)
	ok = true

	if stat, _ := w.file.Stat(); stat != nil {
		if w.currentSize = uint64(stat.Size()); w.currentSize > 0 {
			ok = !w.rotateFunc()
		}
	}

	return ok, nil
}

type Flusher interface {
	Flush() error
}

func (w *RotateFileWriter) Flush() error {
	if w.writer != nil {
		return w.writer.Flush()
	}

	return nil
}

func (w *RotateFileWriter) Close() error {
	if w.writer != nil && w.file != nil {
		_ = w.writer.Flush()
		_ = w.file.Close()
		w.writer = nil
		w.file = nil

		log.Printf("close file %s", w.currentFn)
	}
	return nil
}

var dateFileNameFns = map[*regexp.Regexp]func() string{
	regexp.MustCompile(`(?i)yyyy`): func() string { return time.Now().Format("2006") },
	regexp.MustCompile(`MM`):       func() string { return time.Now().Format("01") },
	regexp.MustCompile(`(?i)dd`):   func() string { return time.Now().Format("02") },
	regexp.MustCompile(`(?i)hh`):   func() string { return time.Now().Format("15") },
	regexp.MustCompile(`mm`):       func() string { return time.Now().Format("04") },
}

func ParseFileNameTemplate(s string) string {
	for r, f := range dateFileNameFns {
		s = r.ReplaceAllString(s, f())
	}

	return s
}

func NewFilename(template string) string {
	fn := ParseFileNameTemplate(template)
	fn = filepath.Clean(fn)

	_, fn = FindMaxFileIndex(fn)
	return fn
}

func RotateFilename(fn string, rotate bool) (string, int) {
	if !rotate {
		return fn, 0
	}

	max, _ := FindMaxFileIndex(fn)
	if max <= 0 {
		return fn, 0
	}

	n := max + 1
	return SetFileIndex(fn, n), n
}

func GetFileIndex(path string) int {
	_, index, _ := SplitBaseIndexExt(path)
	if index == "" {
		return -1
	}

	value, _ := strconv.Atoi(index)
	return value
}

func SetFileIndex(path string, index int) string {
	base, _, ext := SplitBaseIndexExt(path)
	return fmt.Sprintf("%s_%05d%s", base, index, ext)
}

// FindMaxFileIndex finds the max index of a file like log-2021-05-27_00001.log.
// return maxIndex = 0 there is no file matches log-2021-05-27*.log.
// return maxIndex >= 1 tell the max index in matches.
func FindMaxFileIndex(path string) (int, string) {
	base, _, ext := SplitBaseIndexExt(path)
	matches, _ := filepath.Glob(base + "*" + ext)
	if len(matches) == 0 {
		return 0, path
	}

	maxIndex := 1
	maxFn := path
	for _, fn := range matches {
		if index := GetFileIndex(fn); index > maxIndex {
			maxIndex = index
			maxFn = fn
		}
	}

	return maxIndex, maxFn
}

var idx = regexp.MustCompile(`_\d{5,}`)

func SplitBaseIndexExt(path string) (base, index, ext string) {
	if subs := idx.FindAllStringSubmatchIndex(path, -1); len(subs) > 0 {
		sub := subs[len(subs)-1]
		return path[:sub[0]], path[sub[0]+1 : sub[1]], path[sub[1]:]
	}

	ext = filepath.Ext(path)
	return strings.TrimSuffix(path, ext), "", ext
}
