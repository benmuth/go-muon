package muon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestAdd(t *testing.T) {
	db := NewDictBuilder()

	val1 := "hello"

	val2 := []any{"goodbye", "world"}

	val3 := map[string]any{"marco": "polo"}

	db.Add(val1)

	db.Add(val2)

	db.Add(val3)

	if len(db.count) != 5 {
		fmt.Printf("db: %+v\n", db.count)
		t.Fatalf("got: %v, want: %v", len(db.count), 5)
	}
}

func TestAddStr(t *testing.T) {
	db := NewDictBuilder()

	// type test struct {
	// 	str string
	// 	// got int
	// 	want int
	// }

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
		db.Add(test.str)
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

// type jsonData struct {
// 	X map[string]any `json:"-"`
// }

// func TestJSON(t *testing.T) {
// 	b, err := os.ReadFile("../json2mu/simple.json")
// 	if err != nil {
// 		panic(err)
// 	}
// 	fmt.Printf("json: %s\n", b)
// 	x := &jsonData{}
// 	json.Unmarshal(b, x)
// 	// fmt.Println(x)
// 	for k, v := range *x {
// 		fmt.Printf("key: %s, val (%T): %+v\n", k, v, v)
// 	}
// }

// func TestDictBuilder(t *testing.T) {
// 	b, err := os.ReadFile("../json2mu/simple.json")
// 	if err != nil {
// 		panic(err)
// 	}

// 	data := make(map[string]any)
// 	json.Unmarshal(b, &data)

// 	d := NewDictBuilder()
// 	d.Add(data)
// 	table := d.GetDict(512)
// 	if len(table) > 0 {
// 		t.Errorf("somethings in the table!")
// 	}
// }

func TestJSON2Mu(t *testing.T) {
	b, err := os.ReadFile("../json2mu/simple.json")
	if err != nil {
		panic(err)
	}

	data := make(map[string]any)

	json.Unmarshal(b, &data)

	d := NewDictBuilder()
	d.Add(data)
	table := d.GetDict(512)

	out, err := os.Create("../json2mu/simple.mu")
	if err != nil {
		panic(err)
	}

	m := NewMuWriter(out)
	m.TagMuon()
	m.AddLRUDynamic(table)
	m.Add(data)
}

func TestMu2JSON(t *testing.T) {
	f, err := os.Open("../json2mu/simple.mu")
	if err != nil {
		panic(err) // TODO: err handling
	}
	defer f.Close()

	m := NewMuReader(*bufio.NewReader(f))
	b, err := m.inp.Peek(1)
	if err != nil {
		t.Fatalf("didn't peek")
	}
	fmt.Printf("buffered reader peek! %0x\n", b)

	data := m.ReadObject()

	fmt.Printf("DATA\n\n%+v\n", data)

	jsonData, err := json.Marshal(data)
	if err != nil {
		panic(err) // TODO: err handling
	}

	fmt.Printf("OUTPUT\n\n %s\n", jsonData)
}
