package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetFileIndex(t *testing.T) {
	assert.Equal(t, "abc_00002.txt", SetFileIndex("abc_00001.txt", 2))
	assert.Equal(t, "abc_00002.txt", SetFileIndex("abc.txt", 2))
}

func TestGetFileIndex(t *testing.T) {
	assert.Equal(t, 123, GetFileIndex("abc_00123.txt"))
	assert.Equal(t, 123, GetFileIndex("abc_00123.123"))
	assert.Equal(t, -1, GetFileIndex("abc.123"))
}
