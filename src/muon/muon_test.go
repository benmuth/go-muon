package muon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
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
		t.Fatalf("got: %v, want: %v", len(db.count), 5)
	}
}

func TestAddStr(t *testing.T) {
	db := NewDictBuilder()

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
			got := uleb128read(tc.input)
			diff := cmp.Diff(got, tc.want)
			if diff != "" {
				t.Fatalf(diff)
			}
		})
	}
}

func TestLRU(t *testing.T) {
	type lruOp struct {
		op  string
		val any
	}
	tests := []struct {
		name string
		cap  int
		ops  []lruOp
		want []string
	}{
		{
			name: "small",
			cap:  4,
			ops: []lruOp{
				{"add", "string1"},
				{"add", "string2"},
				{"add", "string3"},
				{"add", "string4"},
				{"add", "string5"},
			},
			want: []string{
				"string2",
				"string3",
				"string4",
				"string5",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lru := NewLRU(tc.cap)
			for _, op := range tc.ops {
				if op.op == "add" {
					lru.Append(op.val)
				}
			}
			got := make([]string, 0)
			for _, v := range lru.deque {
				got = append(got, v.(string))
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}

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
//  json.Unmarshal(b, x)
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

// func JSON2Mu() {}

// func TestJSON2Mu(t *testing.T) {
// 	b, err := os.ReadFile("../json2mu/simple.json")
// 	if err != nil {
// 		panic(err)
// 	}

// 	data := make(map[string]any)

// 	json.Unmarshal(b, &data)

// 	d := NewDictBuilder()
// 	d.Add(data)
// 	table := d.GetDict(512)

// 	out, err := os.Create("../json2mu/simple.mu")
// 	if err != nil {
// 		panic(err)
// 	}

// 	m := NewMuWriter(out)
// 	m.TagMuon()
// 	m.AddLRUDynamic(table)
// 	m.Add(data)
// }

// func eqMuON(file1, file2 string) bool {
// 	json1, json2 := Mu2JSON(file1), Mu2JSON(file2)
// 	return eqJSONBytes(json1, json2)
// }

func Mu2JSON(file string) []byte {
	f, err := os.Open(file)
	if err != nil {
		panic(err) // TODO: err handling
	}
	defer f.Close()

	// NOTE: print all bytes
	// b, err := os.ReadFile(file)
	// if err != nil {
	// 	panic(err)
	// }
	// log.Printf("%0 x\n", b)

	m := NewMuReader(*bufio.NewReader(f))

	m.inp.Reset(f)

	data := m.ReadObject()

	jsonData, err := json.Marshal(data)
	if err != nil {
		panic(err) // TODO: err handling
	}
	return jsonData
}

// func TestMu2JSON(t *testing.T) {
// 	muSrc := "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-src.mu"

// 	jsonSrc := "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-src.json"

// 	jsonData, err := os.ReadFile(jsonSrc)
// 	if err != nil {
// 		panic(err)
// 	}
// 	jsonData = bytes.Replace(jsonData, []byte("NaN"), []byte("null"), -1)
// 	jsonData = bytes.Replace(jsonData, []byte("-Infinity"), []byte("null"), -1)
// 	jsonData = bytes.Replace(jsonData, []byte("Infinity"), []byte("null"), -1)
// 	if !json.Valid(jsonData) {
// 		t.Error("INVALID!")
// 		return
// 	}
// 	log.Println("VALID!")

// 	jsonObj := make(map[string]any)

// 	if err := json.Unmarshal(jsonData, &jsonObj); err != nil {
// 		panic(err)
// 	}

// 	// log.Println(jsonObj)

// 	muonFile, err := os.Open(muSrc)
// 	if err != nil {
// 		panic(err)
// 	}

// 	m := NewMuReader(*bufio.NewReader(muonFile))

// 	m.inp.Reset(muonFile)

// 	muonObj := m.ReadObject()

// 	marshalledMuon, err := json.Marshal(muonObj)
// 	if err != nil {
// 		panic(err)
// 	}

// 	unmarshalledMuon := make(map[string]any)
// 	err = json.Unmarshal(marshalledMuon, &unmarshalledMuon)
// 	if err != nil {
// 		panic(err)
// 	}

// 	if s := cmp.Diff(jsonObj, unmarshalledMuon); s != "" {
// 		t.Errorf("DIFF: %s\n", s)
// 	}

// 	// muonObj := make(map[string]any)

// 	// jsonSrcBytes, err := os.ReadFile(jsonSrc)
// 	// if err != nil {
// 	// 	panic(err)
// 	// }

// 	// gotJSON := Mu2JSON(muSrc)

// 	// if !eqJSONBytes(jsonSrcBytes, gotJSON) {
// 	// 	t.Errorf("MuON source: %s\nJSON source: %s\ngot %v\twant %v\n", muSrc, jsonSrc, false, true)
// 	// }
// }

func eqJSONFiles(file1, file2 string) (string, error) {
	b1, err := os.ReadFile(file1)
	if err != nil {
		panic(err)
	}

	b2, err := os.ReadFile(file2)
	if err != nil {
		panic(err)
	}

	// diff := cmp.Diff(b1, b2)

	// if diff != "" {
	// 	log.Println("bytes DIFF: ", diff)
	// 	return false
	// }

	return eqJSONBytes(b1, b2)
}

func eqJSONBytes(bytes1, bytes2 []byte) (string, error) {
	// change invalid JSON values to valid JSON values.
	bytes1 = bytes.Replace(bytes1, []byte("NaN"), []byte("null"), -1)
	bytes1 = bytes.Replace(bytes1, []byte("-Infinity"), []byte("null"), -1)
	bytes1 = bytes.Replace(bytes1, []byte("Infinity"), []byte("null"), -1)

	bytes2 = bytes.Replace(bytes2, []byte("NaN"), []byte("null"), -1)
	bytes2 = bytes.Replace(bytes2, []byte("-Infinity"), []byte("null"), -1)
	bytes2 = bytes.Replace(bytes2, []byte("Infinity"), []byte("null"), -1)

	if !json.Valid(bytes1) {
		err := "file 1 invalid"
		log.Printf(err)
		return "false", fmt.Errorf("Invalid file!: %s", err)
	}

	if !json.Valid(bytes2) {
		err := "file 2 invalid"
		log.Printf(err)
		return "false", fmt.Errorf("Invalid file!: %s", err)
	}
	obj1, obj2 := make(map[string]any), make(map[string]any)

	if err := json.Unmarshal(bytes1, &obj1); err != nil {
		panic(err)
	}

	if err := json.Unmarshal(bytes2, &obj2); err != nil {
		panic(err)
	}

	diff := cmp.Diff(obj1, obj2)
	if diff != "" {
		log.Printf("DIFF: '%v'", diff)
	}
	return diff, nil
}

func TestEqJSON(t *testing.T) {
	tests := []struct {
		name  string
		file1 string
		file2 string
		got   bool
		want  bool
	}{
		{
			name:  "same file",
			file1: "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-src.json",
			file2: "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-src.json",
			want:  true,
		},
		{
			name:  "same file2",
			file1: "/Users/ben/Documents/Programming/go-muon/testdata/tiny/tiny-src.json",
			file2: "/Users/ben/Documents/Programming/go-muon/testdata/tiny/tiny-src.json",
			want:  true,
		},
		{
			name:  "different files",
			file1: "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-src.json",
			file2: "/Users/ben/Documents/Programming/go-muon/testdata/pokemon/pokemon-src.json",
			want:  false,
		},
		{
			name:  "not json",
			file1: "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-src.mu",
			file2: "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-src.json",
			want:  false,
		},

		{
			name:  "python vs original",
			file1: "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-src.json",
			file2: "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-out.json",
			want:  true,
		},
		// {
		// 	name:  "go vs original",
		// 	file1: "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-src.json",
		// 	file2: "/Users/ben/Documents/Programming/go-muon/testdata/sample/sample-go.json",
		// 	want:  true,
		// },
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diff, err := eqJSONFiles(tc.file1, tc.file2)
			if err != nil {
				log.Println(err)
			}

			got := diff == ""

			if got != tc.want {
				t.Errorf("json comparison failed!\nfile1: %s\nfile2: %s\ngot %v \nwant %v\n", tc.file1, tc.file2, got, tc.want)
			}
		})
	}
}
