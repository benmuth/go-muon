package muon

import (
	"bufio"
	"bytes"
	"container/list"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"

	"errors"
)

const MuonMagic = "\x8F\xB5\x30\x31"

//TODO: Handle big endian machines

// NOTE: use zap logger

type DictBuilder struct {
	count map[string]int
}

func NewDictBuilder() *DictBuilder {
	count := make(map[string]int)
	return &DictBuilder{count: count}
}

func (d *DictBuilder) AddStr(s string) {
	// NOTE: why len(val) > 1 check in original code?
	// if len(s) > 1 {
	d.count[s]++
	// }
}

func (d *DictBuilder) Add(x any) {
	fmt.Printf("val: %+v \t type: %T\n", x, x)
	switch val := x.(type) {
	case nil:
		return
	case string:
		d.AddStr(val)
	case []any:
		for _, s := range val {
			d.Add(s)
		}
	case map[string]any:
		for k, v := range val {
			d.AddStr(k)
			d.Add(v)
		}
	}
}

func (d *DictBuilder) GetDict(size int) []string {
	for k, v := range d.count {
		d.count[k] = (v - 1) * len(k) // sets counts of 1 to 0?
	}

	res := make([]string, 0)
	for k, v := range d.count {
		if v > 5 {
			res = append(res, k)
		}
	}
	if len(res) > size {
		res = res[:size]
	}
	return res
}

// integer encoding

func uleb128encode(x int) []byte {
	if x < 0 {
		return []byte{}
	}
	r := make([]byte, 0)
	for {
		b := x & 0x7f
		x >>= 7
		if x == 0 { // NOTE:XXX: won't this always equal 0?
			r = append(r, byte(b))
			return r
		}
		r = append(r, byte(b|0x80))
	}
}

func uleb128decode(b []byte) int {
	r := 0
	for x, e := range b {
		r = r + ((int(e) & 0x7f) << (x * 7))
	}
	return r
}

// TODO: change from reading 1 byte to reading 1 character
func uleb128read(r bufio.Reader) int {
	a := make([]byte, 0)
	for {
		b := make([]byte, 1)
		_, err := r.Read(b)
		if err != nil {
			panic(err) // TODO: Real error handling
		}
		a = append(a, b[0])
		if (b[0] & 0x80) == 0 {
			break
		}
	}
	return uleb128decode(a)
}

func sleb128encode(i int) []byte {
	r := make([]byte, 0)
	for {
		b := i & 0x7f
		i = i >> 7
		if (i == 0 && b&0x40 == 0) || (i == -1 && b&0x40 != 0) {
			r = append(r, byte(b))
			return []byte(r)
		}
		r = append(r, byte(0x80|b))
	}
}

func sleb128decode(b []byte) int {
	r := 0
	for i, e := range b {
		r = r + ((int(e) & 0x7f) << (i * 7))
		if i == len(b)-1 {
			if int(e)&0x40 != 0 {
				r |= -(1<<(i*7) + 7)
			}
		}
	}
	return r
}

func sleb128read(r bufio.Reader) int {
	a := make([]byte, 0)
	for {
		b := []byte{}
		_, err := r.Read(b)
		if err != nil {
			panic(err) // TODO: error handling
		}
		a = append(a, b...)
		if (b[0] & 0x80) == 0 {
			break
		}
	}
	return sleb128decode(a)
}

// Array helpers

// NOTE: detect_array_type is unnecessary, use a type switch in the Writer.add function
// def detect_array_type(arr):
//     if len(arr) < 2:
//         return None

//     res = None
//     if isinstance(arr[0], int):
//         res = 'int'
//     elif isinstance(arr[0], float):
//         res = 'float'

//     for val in arr:
//         if isinstance(val, bool):
//             return None
//         elif isinstance(val, int):
//             pass
//         elif isinstance(val, float):
//             if res == 'int':
//                 res == 'float'  # extend to float
//         else:
//             return None
//     return res

// NOTE: get_array_type_code is used to create an array of a certain type.
func getArrayTypeCode(t byte) rune {
	switch t {
	case 0xB0:
		return 'b'
	case 0xB1:
		return 'h'
	case 0xB2:
		return 'l'
	case 0xB3:
		return 'q'

	case 0xB4:
		return 'B'
	case 0xB5:
		return 'H'
	case 0xB6:
		return 'L'
	case 0xB7:
		return 'Q'

	case 0xB8:
		panic("TypedArray: f16 not supported") //TODO: error handling
	case 0xB9:
		return 'f'
	case 0xBA:
		return 'd'
	default:
		err := errors.New(fmt.Sprintf("No array type for %x", t))
		panic(err)
	}
}

func getTypedArrayMarker(val any) byte {
	switch t := val.(type) {
	case int8:
		return 0xB0
	case int16:
		return 0xB1
	case int32:
		return 0xB2
	case int64:
		return 0xB3

	case uint8:
		return 0xB4
	case uint16:
		return 0xB5
	case uint32:
		return 0xB6
	case uint64:
		return 0xB7

	case float32:
		return 0xB9
	case float64:
		return 0xBA
	default:
		err := errors.New(fmt.Sprintf("No encoding for array %x", t))
		panic(err)
	}
}

// Muon formatter and parser

type muWriter struct {
	out io.ReadWriter

	// this is a deque in python
	// lru *list.List
	lru *LRU

	// NOTE: maybe this can be a slice?
	lruDynamic   *LRU
	detectArrays bool
}

func NewMuWriter(f io.ReadWriter) *muWriter {
	lru := NewLRU(512)
	lruDynamic := NewLRU(512)
	return &muWriter{f, lru, lruDynamic, true} // NOTE: should lruDynamic be a *LRU as well?
}

func (mw *muWriter) TagMuon() {
	mw.write([]byte(MuonMagic))
}

func (mw *muWriter) addTagged(val string, size bool, count bool, pad int) {
	ogOut := mw.out

	// encode to a temporary buffer
	mw.out = bytes.NewBufferString("")
	mw.Add(val)
	out, ok := mw.out.(*bytes.Buffer)
	if !ok {
		panic("Couldn't type assert!")
	}
	buf := make([]byte, len(out.Bytes()))
	n, err := mw.out.Read(buf)
	if err != nil {
		panic(errors.New(fmt.Sprintf("ERROR: failed to read from buffer: %s. %v bytes read", err, n)))
	}
	mw.out = ogOut

	if pad > 0 {
		padding := make([]byte, 0)
		for i := 0; i < pad; i++ {
			padding = append(padding, byte(0xFF))
		}
		mw.write(padding)
	}

	if count {
		b := []byte{0x8A}
		enc := uleb128encode(len(val))
		for _, x := range enc {
			b = append(b, x)
		}

		mw.write(b)
	}

	if size {
		b := []byte{0x8B}
		enc := uleb128encode(len(buf))
		for _, x := range enc {
			b = append(b, x)
		}

		mw.write(b)
	}
}

// NOTE: table might have to be a map, which is then converted to a []string, or a [][]string to correspond to a list of tuples
func (mw *muWriter) AddLRUDynamic(table []string) {
	for _, s := range table {
		last := mw.lruDynamic.list.Back()
		mw.lruDynamic.list.InsertAfter(s, last)
	}
}

func (mw *muWriter) AddLRUList(table []string) {
	for _, s := range table {
		last := mw.lruDynamic.list.Back()
		mw.lruDynamic.list.InsertAfter(s, last)
	}

	mw.write([]byte{0x8C})
	mw.startList()
	for _, s := range table {
		mw.write(append([]byte(s), 0x00))
	}
}

func (mw *muWriter) Add(value any) {
	switch val := value.(type) {
	case string:
		mw.addStr(val)
	case nil:
		mw.write([]byte{0xAC})
	case bool:
		var b byte
		if val {
			b = byte(0xAB)
		} else {
			b = byte(0xAA)
		}
		mw.write([]byte{b})
	case int:
		if val >= 0 && val <= 9 {
			mw.write([]byte{byte(0xA0 + val)})
		} else {
			enc := sleb128encode(val)
			lenc := len(enc)
			if val < 0 {
				switch {
				case val >= -0x80:
					mw.write([]byte{0xB0, byte(int8(val))})
				case val >= -0x8000 && lenc >= 2:
					mw.write([]byte{0xB1, byte(int16(val))})
				case val >= -0x8000_0000 && lenc >= 4:
					mw.write([]byte{0xB2, byte(int32(val))})
				case val >= -0x8000_0000_0000_0000 && lenc >= 8:
					mw.write([]byte{0xB3, byte(int64(val))})
				default:
					mw.write(append([]byte{0xBB}, enc...))
				}
			} else {
				switch {
				case val < 0x80:
					mw.write([]byte{0xB4, byte(uint8(val))})
				case val < 0x8000 && lenc >= 2:
					mw.write([]byte{0xB5, byte(uint16(val))})
				case val < 0x8000_0000 && lenc >= 4:
					mw.write([]byte{0xB6, byte(uint32(val))})
				case val < 0x7FFF_FFFF_FFFF_FFFF && lenc >= 8:
					mw.write([]byte{0xB7, byte(uint64(val))})
				default:
					mw.write(append([]byte{0xBB}, enc...))
				}
			}
		}
	case float64: //TODO: handle float16, float32
		if math.IsNaN(val) {
			mw.write([]byte{0xAD})
			return
		}
		if math.IsInf(val, 0) {
			var b byte
			if val < 0 {
				b = 0xAE
			} else {
				b = 0xAF
			}
			mw.write([]byte{b})
		}

		mw.write(binary.LittleEndian.AppendUint64([]byte{0xBA}, math.Float64bits(val)))
	case []string: // TODO: handle different types of arrays
		code := getTypedArrayMarker(val)
		mw.write([]byte{0x84, code})
		mw.write(uleb128encode(len(val)))
		// TODO: handle big endian machines
		n, err := io.WriteString(mw.out, strings.Join(val, ""))
		if err != nil {
			panic(errors.New(fmt.Sprintf("ERROR: failed to write to output: %s. %v bytes written", err, n)))
		}
	case []byte:
		mw.write([]byte{0x84, 0xB4})
		mw.write(uleb128encode(len(val)))
		mw.write(val)
	case []int:
		mw.write([]byte{0x84, 0xBB})
		mw.write(uleb128encode(len(val)))
		for _, v := range val {
			mw.write(sleb128encode(v))
		}
		return
	case []float32, []float64:
		if mw.detectArrays {
			mw.Add(val) // NOTE: infinite recursion?
			return
		}
	case []any:
		mw.startList()
		for _, v := range val {
			mw.Add(v)
		}
		mw.endList()
	case map[string]any:
		mw.startDict()
		for k, v := range val {
			mw.addStr(k)
			mw.Add(v)
		}
		mw.endDict()
	}
	return
}

func (mw *muWriter) write(b []byte) {
	n, err := mw.out.Write(b)
	if err != nil {
		panic(errors.New(fmt.Sprintf("ERROR: failed to write to buffer: %s. %v bytes written", err, n)))
	}
}

// Low-level API

func (mw *muWriter) addStr(value any) {
	val, ok := value.(string)
	if !ok {
		panic("couldn't cast val to string!")
	}
	// strlen := len(val)

	i := mw.lru.FindIndex(val)
	if i >= 0 { // TODO: use map to check membership
		idx := mw.lru.list.Len() - i - 1
		mw.write(append([]byte{0x81}, uleb128encode(idx)...))
	} else {
		if mw.lruDynamic.FindIndex(val) >= 0 {
			mw.lru.list.InsertAfter(val, mw.lru.list.Back())
			mw.lruDynamic.list.Remove(value.(*list.Element))
			mw.write([]byte{0x8C})
		}

		if strings.Contains(val, "\x00") || len(val) >= 512 {
			mw.write([]byte{0x82})
			mw.write(uleb128encode(len(val)))
			mw.write([]byte(val))
		} else {
			mw.write(append([]byte(val), 0x00))
		}
	}

}

func (mw *muWriter) append(b byte) {
	mw.write([]byte{b})
}

func (mw *muWriter) startList() { mw.append(0x90) }
func (mw *muWriter) endList()   { mw.append(0x91) }
func (mw *muWriter) startDict() { mw.append(0x92) }
func (mw *muWriter) endDict()   { mw.append(0x93) }

func (mw *muWriter) startArray(val []any, chunked bool) {
	code := getTypedArrayMarker(val)
	var b byte
	if chunked {
		b = 0x85
	} else {
		b = 0x84
	}
	mw.write([]byte{b, code})
	mw.addArrayChunk(val)
}

func (mw *muWriter) addArrayChunk(val []any) {
	mw.write(uleb128encode(len(val)))
	// TODO: handle BIG_ENDIAN
	for _, v := range val {
		mw.write([]byte{v.(byte)})
	}
}

func (mw *muWriter) endArrayChunked() {
	mw.append(0x00)
}

//TODO: handle float16
// func (mw *muWriter) addTypedArrayF16(val []float64) {
// 	mw.write([]byte{0x84, 0xB8})
// 	mw.write(uleb128encode(len(val)))
// 	for _, v := range val {
// 		mw.write()
// 	}
// }

type muReader struct {
	inp bufio.Reader
	lru *LRU
}

func NewMuReader(inp bufio.Reader) *muReader {
	return &muReader{inp, NewLRU(512)} //NOTE: what should capacity of LRU be?
}

func (mr *muReader) peekByte() byte {
	b, err := mr.inp.Peek(1)
	if err != nil {
		panic(err) //TODO: err handling
	}
	return b[0]
}

// NOTE: not used?
func (mr *muReader) hasData() bool {
	return mr.inp.Buffered() > 0
}

func (mr *muReader) readString() (res string) {
	tag := make([]byte, 1)
	_, err := mr.inp.Read(tag)
	if err != nil {
		panic(err) // TODO: err handling
	}
	switch c := tag[0]; c {
	case 0x81:
		n := uleb128read(mr.inp)
		for i, e := 0, mr.lru.list.Back(); i < n; i++ {
			res = e.Value.(string)
			e = e.Prev()
		}
		return
	case 0x82:
		n := uleb128read(mr.inp)
		b := make([]byte, n)
		_, err := mr.inp.Read(b)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return string(b)
	default:
		res, err = mr.inp.ReadString(0x00)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return
	}
}

func (mr *muReader) readSpecial() any {
	data := make([]byte, 1)
	_, err := mr.inp.Read(data)
	if err != nil {
		panic(err) // TODO: err handling
	}
	switch t := data[0]; t {
	case 0xAA:
		return false
	case 0xAB:
		return true
	case 0xAC:
		return nil
	case 0xAD:
		return math.NaN()
	case 0xAE:
		return math.Inf(-1)
	case 0xAF:
		return math.Inf(1)
	case 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7, 0xA8, 0xA9:
		return t - 0xA0
	default:
		panic("Wrong special value!")
	}
}

func (mr *muReader) readTypedValue() any {
	data := make([]byte, 1)
	_, err := mr.inp.Read(data)
	if err != nil {
		panic(err) // TODO: err handling
	}
	switch t := data[0]; t {
	case 0xB0:
		//     res = struct.unpack('<b', self.inp.read(1))[0]
		res, err := mr.inp.ReadByte()
		if err != nil {
			panic(err) // TODO: err handling
		}
		return int8(res)
	case 0xB1:
		data := make([]byte, 2)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return int16(binary.LittleEndian.Uint16(data))
	case 0xB2:
		data := make([]byte, 4)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return int32(binary.LittleEndian.Uint32(data))
	case 0xB3:
		data := make([]byte, 8)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return int64(binary.LittleEndian.Uint64(data))
	case 0xB4:
		res, err := mr.inp.ReadByte()
		if err != nil {
			panic(err) // TODO: err handling
		}
		return uint8(res)
	case 0xB5:
		//     res = struct.unpack('<H', self.inp.read(2))[0]
		data := make([]byte, 2)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return binary.LittleEndian.Uint16(data)
	case 0xB6:
		//     res = struct.unpack('<L', self.inp.read(4))[0]
		data := make([]byte, 4)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return binary.LittleEndian.Uint32(data)
	case 0xB7:
		//     res = struct.unpack('<Q', self.inp.read(8))[0]
		data := make([]byte, 8)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return (binary.LittleEndian.Uint64(data))
	case 0xB8:
		panic("Not supporting f16!") // TODO: support f16
	case 0xB9:
		data := make([]byte, 4)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return math.Float32frombits(binary.LittleEndian.Uint32(data))
	case 0xBA:
		data := make([]byte, 8)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(data))
	case 0xBB:
		return sleb128read(mr.inp)
	default:
		panic("Unknown typed value")
	}
}

func (mr *muReader) readTypedArray() []any {
	data, err := mr.inp.ReadByte()
	if err != nil {
		panic(err) // TODO: err handling
	}

	var chunked bool
	if data == 0x85 {
		chunked = true
	}

	t, err := mr.inp.ReadByte()
	if err != nil {
		panic(err) // TODO: err handling
	}

	var res []any
	// var chunk []any
	switch t {
	case 0xBB:
		res := make([]byte, 0)
		for {
			n := uleb128read(mr.inp)
			if n == 0 {
				break
			}

			for i := 0; i < n; i++ {
				val := sleb128read(mr.inp)
				res = append(res, byte(val))
			}
			if !chunked {
				ret := make([]any, len(res))
				for i, v := range res {
					ret[i] = v
				}
				return ret
			}
		}
	case 0xB8:
		// TODO: support f16
		panic("f16 not supported")
		// for i := 0; i < n; i++ {
		// 	data := make([]byte, 2)
		// 	_, err := mr.inp.Read(data)
		// 	if err != nil {
		// 		panic(err) // TODO: err handling
		// 	}
		// }
	default:
		for {
			n := uleb128read(mr.inp)
			if n == 0 {
				break
			}

			chunk := make([]byte, n)
			_, err := mr.inp.Read(chunk)
			if err != nil {
				panic(err) // TODO: err handling
			}
			if !chunked {
				ret := make([]any, n)
				for i, v := range chunk {
					ret[i] = v
				}
				return ret
			}
			// TODO: handle big endian
			for _, v := range chunk {
				res = append(res, v)
			}
		}
	}
	return res
}

func (mr *muReader) readList() []any {
	res := make([]any, 0)
	b, err := mr.inp.ReadByte()
	if err != nil {
		panic(err) // TODO: err handling
	}
	if b != 0x90 {
		panic("not a list start!") //TODO: err handling
	}
	next, err := mr.inp.Peek(1)
	if err != nil {
		panic(err) // TODO: err handling
	}
	for next[0] != 0x91 {
		res = append(res, mr.readObject()...)
		next, err = mr.inp.Peek(1)
		if err != nil {
			panic(err) // TODO: err handling
		}
	}
	_, err = mr.inp.ReadByte()
	if err != nil {
		panic(err) // TODO: err handling
	}
	return res
}

func (mr *muReader) readDict() map[any]any {
	res := make(map[any]any)
	b, err := mr.inp.ReadByte()
	if err != nil {
		panic(err) // TODO: err handling
	}
	if b != 0x92 {
		panic("not a dict start!") //TODO: err handling
	}

	next, err := mr.inp.Peek(1)
	if err != nil {
		panic(err) // TODO: err handling
	}
	for next[0] != 0x93 {
		key := mr.readObject()
		val := mr.readObject()
		res[key] = val
		next, err = mr.inp.Peek(1)
		if err != nil {
			panic(err) // TODO: err handling
		}
	}
	_, err = mr.inp.ReadByte()
	if err != nil {
		panic(err) // TODO: err handling
	}
	return res
}

func (mr *muReader) readObject() []any {
	data, err := mr.inp.Peek(1)
	if err != nil {
		panic(err) // TODO: err handling
	}
	nxt := data[0]

	for nxt == 0xFF {
		data, err = mr.inp.Peek(1)
		if err != nil {
			panic(err) // TODO: err handling
		}
		nxt = data[0]
	}
	var ret []any
	if nxt > 0x82 && nxt <= 0xC1 {
		switch {
		case nxt >= 0xA0 && nxt <= 0xAF:
			return append(ret, mr.readSpecial())
		case nxt >= 0xB0 && nxt <= 0xBB:
			return append(ret, mr.readTypedValue())
		case nxt == 0x84 || nxt == 0x85:
			return mr.readTypedArray()
		case nxt == 0x8A:
			_, err := mr.inp.ReadByte()
			if err != nil {
				panic(err) // TODO: err handling
			}
			_ = uleb128read(mr.inp)
			return mr.readObject()
		case nxt == 0x8B:
			_, err := mr.inp.ReadByte()
			if err != nil {
				panic(err) // TODO: err handling
			}
			_ = uleb128read(mr.inp)
			return mr.readObject()
		case nxt == 0x8C:
			_, err := mr.inp.ReadByte()
			if err != nil {
				panic(err) // TODO: err handling
			}
			if mr.peekByte() == 0x90 {
				// mr.lru.extend(mr.readList())
				for _, v := range mr.readList() {
					mr.lru.list.InsertAfter(v, mr.lru.list.Back())
				}
				// Read next object (LRU list is skipped)
				return mr.readObject()
			} else {
				str := mr.readString()
				mr.lru.list.InsertAfter(str, mr.lru.list.Back())
				res := make([]any, len(str))
				for i, c := range str {
					res[i] = c
				}
				return res
			}
		case nxt == 0x8F:
			// assert mr.inp.read(4) == MuonMagic
			data := make([]byte, 4)
			_, err := mr.inp.Read(data)
			if err != nil {
				panic(err) // TODO: err handling
			}
			for i, b := range MuonMagic {
				if byte(b) != data[i] {
					panic("not muon magic!")
				}
			}
			return mr.readObject()
		case nxt == 0x90:
			return mr.readList()
		case nxt == 0x92:
			dict := mr.readDict()
			res := make([]any, len(dict))
			for k, v := range dict {
				res = append(res, dictRecord{k, v})
			}
			return res
		default:
			panic("Unknown object")
		}
	}
	return []any{}
}

type dictRecord struct {
	key any
	val any
}

// func dumps(data)

// func loads(data)
