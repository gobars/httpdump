package util

import (
	"errors"
	"strconv"
	"strings"
)

// ParseIntSet parses str like 200, 200-300 or 200,300-400 to a IntSet.
func ParseIntSet(str string) (*IntSet, error) {
	var intSet IntSet
	for _, item := range strings.Split(str, ",") {
		var ns = strings.Split(item, "-")
		if len(ns) > 2 {
			return nil, errors.New("illegal range str: " + item)
		}
		var start, end int
		var err error
		if len(ns) > 0 {
			start, err = strconv.Atoi(ns[0])
			end = start
		}
		if len(ns) > 1 {
			end, err = strconv.Atoi(ns[1])
		}
		if err != nil {
			return nil, err
		}

		if start > end {
			start, end = end, start
		}

		intSet.ranges = append(intSet.ranges, NewIntRange(start, end))
	}
	return &intSet, nil
}

// IntSet is a set of int values.
type IntSet struct {
	ranges []IntRange
}

// NewIntSet creates new IntSet.
func NewIntSet(ranges ...IntRange) *IntSet {
	return &IntSet{ranges: ranges}
}

// String implements Stringer.
func (s IntSet) String() string {
	var sb strings.Builder
	for index, r := range s.ranges {
		if index > 0 {
			sb.WriteRune(',')
		}
		if r.Start == r.End {
			sb.WriteString(strconv.Itoa(r.Start))
		} else {
			sb.WriteString(strconv.Itoa(r.Start))
			sb.WriteRune('-')
			sb.WriteString(strconv.Itoa(r.End))
		}
	}
	return sb.String()
}

// Contains checks if this set contains int value.
func (s IntSet) Contains(value int) bool {
	if len(s.ranges) == 0 {
		return true
	}
	for _, r := range s.ranges {
		if r.Contains(value) {
			return true
		}
	}
	return false
}

// IntRange is a ange of int value.
type IntRange struct {
	Start, End int // inclusive
}

// NewIntRange create new int range.
func NewIntRange(start int, end int) IntRange { return IntRange{Start: start, End: end} }

// Contains checks if this range contains the value.
func (r IntRange) Contains(value int) bool { return value >= r.Start && value <= r.End }
