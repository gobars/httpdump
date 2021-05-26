package main

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
	if w.file == nil {
		fn := FirstFilename(w.FnTemplate)
		w.currentFn = fn
		w.currentSize = FileSize(fn)
	}

	if fn := RotateFilename(w.currentFn, w.rotateFunc()); fn != w.currentFn || w.file == nil {
		if err := w.openFile(fn); err != nil {
			return 0, err
		}
	}

	n, err := w.writer.Write(p)
	w.currentSize += uint64(n)
	return n, err
}

func (w *RotateFileWriter) openFile(fn string) (err error) {
	_ = w.Close()

	w.file, err = os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0660)
	if err != nil {
		return err
	}

	w.currentFn = fn
	w.writer = bufio.NewWriter(w.file)
	return nil
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
		w.currentSize = 0
		w.writer = nil
		w.file = nil

		log.Printf("close file %s", w.currentFn)
	}
	return nil
}

func FileSize(fn string) uint64 {
	stat, err := os.Stat(fn)
	if err != nil {
		return 0
	}

	return uint64(stat.Size())
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

func FirstFilename(template string) string {
	fn := ParseFileNameTemplate(template)
	fn = filepath.Clean(fn)

	max := FindMaxFileIndex(fn)
	if max < 0 {
		return fn
	}

	return SetFileIndex(fn, max+1)
}

func RotateFilename(fn string, rotate bool) string {
	if !rotate {
		return fn
	}

	max := FindMaxFileIndex(fn)
	if max < 0 {
		return fn
	}

	return SetFileIndex(fn, max+1)
}

var idx = regexp.MustCompile(`_\d+$`)

func GetFileIndex(path string) int {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	subs := idx.FindStringSubmatch(base)
	if len(subs) == 0 {
		return -1
	}

	index, _ := strconv.Atoi(subs[len(subs)-1][1:])
	return index
}

func SetFileIndex(path string, index int) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	loc := idx.FindStringSubmatchIndex(base)
	if len(loc) == 0 {
		return fmt.Sprintf("%s_%05d%s", base, index, ext)
	}

	return fmt.Sprintf("%s_%05d%s", base[:loc[0]], index, ext)

}

func FindMaxFileIndex(path string) int {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	loc := idx.FindStringSubmatchIndex(base)
	if len(loc) > 0 {
		base = base[:loc[0]]
	}

	matches, _ := filepath.Glob(base + "*" + ext)
	if len(matches) == 0 {
		return -1
	}

	maxIndex := 0
	for _, match := range matches {
		if index := GetFileIndex(match); index > maxIndex {
			maxIndex = index
		}
	}

	return maxIndex
}
