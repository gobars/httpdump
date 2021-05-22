package main

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Option Command line options.
type Option struct {
	Level     string        `val:"header" usage:"Output level, options are: url(only url) | header(http headers) | all(headers, and textuary http body)"`
	File      string        `usage:"Read from pcap file. If not set, will capture data from network device by default"`
	Device    string        `val:"any" usage:"Capture packet from network device. If is any, capture all interface traffics"`
	Ip        string        `usage:"Filter by ip, if either source or target ip is matched, the packet will be processed"`
	Port      uint          `usage:"Filter by port, if either source or target port is matched, the packet will be processed."`
	Chan      uint          `val:"10240" usage:"Channel size to buffer tcp packets."`
	Host      string        `usage:"Filter by request host, using wildcard match(*, ?)"`
	Uri       string        `usage:"Filter by request url path, using wildcard match(*, ?)"`
	PrintResp bool          `usage:"Print response or not"`
	Status    string        `usage:"Filter by response status code. Can use range. eg: 200, 200-300 or 200:300-400"`
	StatusSet *IntSet       `flag:"-"`
	Force     bool          `usage:"Force print unknown content-type http body even if it seems not to be text content"`
	Curl      bool          `usage:"Output an equivalent curl command for each http request"`
	DumpBody  bool          `usage:"dump http request/response body to file"`
	Output    string        `usage:"Write result to file [output] instead of stdout"`
	Idle      time.Duration `val:"4m" usage:"Idle time to remove connection if no package received"`
}

// parse int set
func ParseIntSet(str string) (*IntSet, error) {
	if str == "" {
		return nil, errors.New("empty str")
	}
	var intSet IntSet
	for _, item := range strings.Split(str, ":") {
		var numbers = strings.Split(item, "-")
		if len(numbers) > 2 {
			return nil, errors.New("illegal range str: " + item)
		}
		if len(numbers) == 1 {
			start, err := strconv.Atoi(numbers[0])
			if err != nil {
				return nil, err
			}
			intSet.ranges = append(intSet.ranges, NewIntRange(start, start))
		} else if len(numbers) == 2 {
			start, err := strconv.Atoi(numbers[0])
			if err != nil {
				return nil, err
			}
			end, err := strconv.Atoi(numbers[1])
			if err != nil {
				return nil, err
			}
			intSet.ranges = append(intSet.ranges, NewIntRange(start, end))
		}
	}
	return &intSet, nil
}

// A set of int values
type IntSet struct {
	ranges []IntRange
}

// Create new IntSet
func NewIntSet(ranges ...IntRange) *IntSet {
	return &IntSet{
		ranges: ranges,
	}
}

// implement Stringer
func (s *IntSet) String() string {
	var sb strings.Builder
	for index, r := range s.ranges {
		if index > 0 {
			sb.WriteRune(':')
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

// If this set contains int value
func (s *IntSet) Contains(value int) bool {
	for _, r := range s.ranges {
		if r.Contains(value) {
			return true
		}
	}
	return false
}

// Range of int value
type IntRange struct {
	Start int // inclusive
	End   int // inclusive
}

// Create new int range
func NewIntRange(start int, end int) IntRange {
	return IntRange{
		Start: start,
		End:   end,
	}
}

// If this range contains the value
func (r *IntRange) Contains(value int) bool {
	return value >= r.Start && value <= r.End
}
