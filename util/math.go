package util

var EPSILON = 0.00000001

func Float64Equals(a, b float64) bool {
	return (a-b) < EPSILON && (b-a) < EPSILON
}
