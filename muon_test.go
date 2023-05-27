package muon

import (
	"bufio"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestAdd(t *testing.T) {
	db := New()

	val1 := "hello"

	val2 := []string{"goodbye", "world"}

	val3 := map[string]string{"marco": "polo"}

	db.add(val1)

	db.add(val2)

	db.add(val3)

	fmt.Println(db.count)
}

func TestAddStr(t *testing.T) {
	db := New()

	type test struct {
		str string
		// got int
		want int
	}

	tests := []struct {
		str string
		// got int
		want int
	}{
		{"a", 1},
		{"b", 1},
		{"a", 2},
		{"c", 1},
		{"a", 3},
	}

	for _, test := range tests {
		db.add(test.str)
		got := db.count[test.str]

		if got != test.want {
			t.Fatalf("expected: %v, got %v\n", test.want, got)
		}
	}
}

func TestUleb128Encode(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  []byte
	}{
		{"encode zero", 0, []byte{0}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := uleb128encode(tc.input)
			for i, b := range got {
				if b != tc.want[i] {
					t.Fatalf("got %0b, want %0b for byte index %v\n", b, tc.want[i], i)
				}
			}
		})
	}
}

func TestUleb128Decode(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  int
	}{
		{"decode zero", []byte{0}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := uleb128decode(tc.input)
			diff := cmp.Diff(got, tc.want)
			if diff != "" {
				t.Fatalf(diff)
			}
		})
	}
}

type byteReader []byte

func (br byteReader) Read(b []byte) (int, error) {
	l := len(b)
	b = br
	return l, nil
}

func TestUleb128Read(t *testing.T) {
	tests := []struct {
		name  string
		input *bufio.Reader
		want  int
	}{
		{"read zero", bufio.NewReader(byteReader{0}), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := uleb128read(*tc.input)
			diff := cmp.Diff(got, tc.want)
			if diff != "" {
				t.Fatalf(diff)
			}
		})
	}
}

// func TestLRU(t *testing.T) {
// 	tests := []struct {
// 		name  string
// 		input io.Reader
// 		want  int
// 	}{
// 		{"read zero", byteReader{0}, 0},
// 	}
// }