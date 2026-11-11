package layout

import (
	"reflect"
	"testing"
)

func TestSelectPagesParity(t *testing.T) {
	cases := []struct {
		rangeText string
		parity    string
		total     int
		want      []int
	}{
		{"", "all", 5, []int{1, 2, 3, 4, 5}},
		{"", "odd", 5, []int{1, 3, 5}},
		{"", "even", 5, []int{2, 4}},
		{"1-10", "odd", 10, []int{1, 3, 5, 7, 9}},
		{"1-10", "even", 10, []int{2, 4, 6, 8, 10}},
		{"2-5,8", "odd", 10, []int{3, 5}},
	}
	for _, tc := range cases {
		if got := SelectPages(tc.total, tc.rangeText, tc.parity); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SelectPages(%d, %q, %q) = %v, want %v", tc.total, tc.rangeText, tc.parity, got, tc.want)
		}
	}
}

func TestCompactPages(t *testing.T) {
	cases := []struct {
		in   []int
		want string
	}{
		{[]int{1, 3, 5, 7, 9}, "1,3,5,7,9"},
		{[]int{1, 2, 3, 5, 6}, "1-3,5-6"},
		{[]int{2, 4, 6, 8}, "2,4,6,8"},
	}
	for _, tc := range cases {
		if got := CompactPages(tc.in); got != tc.want {
			t.Errorf("CompactPages(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
