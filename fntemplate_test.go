package main

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSetFileIndex(t *testing.T) {
	assert.Equal(t, "abc_2.txt", SetFileIndex("abc_1.txt", 2))
	assert.Equal(t, "abc_2.txt", SetFileIndex("abc.txt", 2))
}

func TestGetFileIndex(t *testing.T) {
	assert.Equal(t, 123, GetFileIndex("abc_123.txt"))
	assert.Equal(t, 123, GetFileIndex("abc_123.123"))
	assert.Equal(t, -1, GetFileIndex("abc.123"))
}
