package muon

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"strings"

	"github.com/x448/float16"
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
	d.count[s]++
}

func (d *DictBuilder) Add(x any) {
	// fmt.Printf("val: %+v \t type: %T\n", x, x)
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
func uleb128read(r *bufio.Reader) int {
	a := make([]byte, 0)
	for {
		b, err := r.ReadByte()
		if err != nil {
			panic(err) // TODO: Real error handling
		}
		// log.Printf("next char: %x", b)
		a = append(a, b)
		if (b & 0x80) == 0 {
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

func sleb128decode(b []byte) *big.Int {
	r := big.NewInt(0)
	var i int
	var e byte
	for i, e = range b {
		// r = r + ((int(e) & 0x7f) << (i * 7))
		x := big.NewInt(int64(e & 0x7f))
		y := x.Lsh(x, uint(i*7))
		r.Add(r, y)
	}
	if int(e)&0x40 != 0 { // check if the one bit of the byte is set
		z := big.NewInt(1)
		z = z.Lsh(z, uint(i*7+7))
		z.Neg(z)
		// r |= - (1 << (i * 7) + 7
		r = r.Or(r, z)
	}
	return r
}

func sleb128read(r *bufio.Reader) *big.Int {
	a := make([]byte, 0)
	for {
		b, err := r.ReadByte()
		if err != nil {
			panic(err) // TODO: error handling
		}
		a = append(a, b)
		if (b & 0x80) == 0 {
			break
		}
	}
	return sleb128decode(a)
}

// Array helpers

func getTypeWidth(typeCode byte) int {
	width := 0
	switch typeCode {
	case 0xB0, 0xB4: // i8, u8
		width = 1
	case 0xB1, 0xB5: // i16, u16
		width = 2
	case 0xB9: // f32, encoded as 4 bytes!
		width = 4
	case 0xB2, 0xB6: //i32, u32
		// HACK: it seems like 32 bits are encoded with 8 bytes in Python implementation?
		// width = 4 // this doesn't work!
		width = 8
	case 0xB3, 0xB7, 0xBA: // i64, u64, f64
		width = 8
	case 0xB8:
		panic("TypedArray: f16 not supported") //TODO: error handling
	default:
		panic("No array for type")
	}
	return width
}

func readBitsAs(typeCode byte, bits []byte) any {
	width := getTypeWidth(typeCode)
	bits = bits[:width]
	switch typeCode {
	case 0xB0: // i8
		return int8(bits[0])
	case 0xB1: // i16
		return int16(binary.LittleEndian.Uint16(bits))
	case 0xB2: // i32
		return int32(binary.LittleEndian.Uint32(bits))
	case 0xB3: // i64
		return int64(binary.LittleEndian.Uint64(bits))
	case 0xB4: // u8
		return uint8(bits[0])
	case 0xB5: // u16
		return binary.LittleEndian.Uint16(bits)
	case 0xB6: // u32
		return binary.LittleEndian.Uint32(bits)
	case 0xB7: // u64
		return binary.LittleEndian.Uint64(bits)

	case 0xB8: // f16
		panic("TypedArray: f16 not supported") //TODO: error handling
	case 0xB9: // f32
		return math.Float32frombits(binary.LittleEndian.Uint32(bits))
	case 0xBA: // f64
		return math.Float64frombits(binary.LittleEndian.Uint64(bits))
	default:
		// err := errors.New(fmt.Sprintf("No array type for %x", t))
		panic("No array for type")
	}
}

func readArrayFromBits(typeCode byte, data []byte) any {
	width := getTypeWidth(typeCode)
	ret := make([]any, 0)

	for i := 0; i < len(data); i += width {
		v := readBitsAs(typeCode, data[i:i+width])
		ret = append(ret, v)
	}
	return ret
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
		err := fmt.Errorf("no encoding for array %x", t)
		panic(err)
	}
}

// Muon formatter and parser

type muWriter struct {
	out          io.ReadWriter
	lru          *LRU
	lruDynamic   *LRU
	detectArrays bool
}

func NewMuWriter(f io.ReadWriter) *muWriter {
	lru := NewLRU(512)
	lruDynamic := NewLRU(512)
	return &muWriter{f, lru, lruDynamic, true}
}

func (mw *muWriter) TagMuon() {
	mw.write([]byte(MuonMagic))
}

// func (mw *muWriter) addTagged(val string, size bool, count bool, pad int) {
// 	ogOut := mw.out

// 	// encode to a temporary buffer
// 	mw.out = bytes.NewBufferString("")
// 	mw.Add(val)
// 	out, ok := mw.out.(*bytes.Buffer)
// 	if !ok {
// 		panic("Couldn't type assert!")
// 	}
// 	buf := make([]byte, len(out.Bytes()))
// 	n, err := mw.out.Read(buf)
// 	if err != nil {
// 		panic(errors.New(fmt.Sprintf("ERROR: failed to read from buffer: %s. %v bytes read", err, n)))
// 	}
// 	mw.out = ogOut

// 	if pad > 0 {
// 		padding := make([]byte, 0)
// 		for i := 0; i < pad; i++ {
// 			padding = append(padding, byte(0xFF))
// 		}
// 		mw.write(padding)
// 	}

// 	if count {
// 		b := []byte{0x8A}
// 		enc := uleb128encode(len(val))
// 		for _, x := range enc {
// 			b = append(b, x)
// 		}

// 		mw.write(b)
// 	}

// 	if size {
// 		b := []byte{0x8B}
// 		enc := uleb128encode(len(buf))
// 		for _, x := range enc {
// 			b = append(b, x)
// 		}

// 		mw.write(b)
// 	}
// }

func (mw *muWriter) AddLRUDynamic(table []any) {
	mw.lruDynamic.Extend(table)
}

func (mw *muWriter) AddLRUList(table []any) {
	mw.lru.Extend(table)

	mw.write([]byte{0x8C})
	mw.startList()
	for _, s := range table {
		mw.write(append([]byte(s.(string)), 0x00))
	}
	mw.endList()
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
			panic(fmt.Errorf("ERROR: failed to write to output: %s. %v bytes written", err, n))
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
	// return
}

func (mw *muWriter) write(b []byte) {
	n, err := mw.out.Write(b)
	if err != nil {
		panic(fmt.Errorf("ERROR: failed to write to buffer: %s. %v bytes written", err, n))
	}
}

// Low-level API

func (mw *muWriter) addStr(value any) {
	val := value.(string)

	valIdx := mw.lru.FindIndex(val)
	if valIdx >= 0 { // TODO: use map to check membership
		idx := len(mw.lru.deque) - valIdx - 1
		mw.write(append([]byte{0x81}, uleb128encode(idx)...))
	} else {
		if mw.lruDynamic.FindIndex(val) >= 0 {
			mw.lru.Append(val)
			mw.lruDynamic.Remove(val)
			mw.write([]byte{0x8C})
		}

		buff := []byte(val)
		if bytes.Contains(buff, []byte{0}) || len(val) >= 512 {
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

// func (mw *muWriter) startArray(val []any, chunked bool) {
// 	code := getTypedArrayMarker(val)
// 	var b byte
// 	if chunked {
// 		b = 0x85
// 	} else {
// 		b = 0x84
// 	}
// 	mw.write([]byte{b, code})
// 	mw.addArrayChunk(val)
// }

// func (mw *muWriter) addArrayChunk(val []any) {
// 	mw.write(uleb128encode(len(val)))
// 	// TODO: handle BIG_ENDIAN
// 	for _, v := range val {
// 		mw.write([]byte{v.(byte)})
// 	}
// }

// func (mw *muWriter) endArrayChunked() {
// 	mw.append(0x00)
// }

//TODO: handle float16
// func (mw *muWriter) addTypedArrayF16(val []float64) {
// 	mw.write([]byte{0x84, 0xB8})
// 	mw.write(uleb128encode(len(val)))
// 	for _, v := range val {
// 		mw.write()
// 	}
// }

type muReader struct {
	inp *bufio.Reader
	lru *LRU
}

func NewMuReader(inp bufio.Reader) *muReader {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return &muReader{&inp, NewLRU(512)} //NOTE: what should capacity of LRU be?
}

func (mr *muReader) peekByte() byte {
	b, err := mr.inp.Peek(1)
	if err != nil {
		panic(err) //TODO: err handling
	}
	return b[0]
}

// NOTE: not used?
// func (mr *muReader) hasData() bool {
// 	return mr.inp.Buffered() > 0
// }

func (mr *muReader) readString() (res string) {
	c, err := mr.inp.ReadByte()
	if err != nil {
		panic(err) // TODO: err handling
	}
	// log.Printf("next char: %x", c)
	switch c {
	case 0x81:
		n := uleb128read(mr.inp)
		res = mr.lru.Get(-n).(string)
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
		buff := make([]byte, 0)
		for c != 0x00 {
			buff = append(buff, c)
			c, err = mr.inp.ReadByte()
			if err != nil {
				panic(err)
			}
			// log.Printf("next char: %x", c)
		}
		res = string(buff)
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
		// return math.NaN() // NOTE: NaN not valid JSON! replacing with nil
		return nil
	case 0xAE:
		// return math.Inf(-1) // NOTE: Invalid JSON! replacing with nil
		return nil
	case 0xAF:
		// return math.Inf(1) // NOTE: Invalid JSON: replacing with nil
		return nil
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
		data := make([]byte, 2)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return binary.LittleEndian.Uint16(data)
	case 0xB6:
		data := make([]byte, 4)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return binary.LittleEndian.Uint32(data)
	case 0xB7:
		data := make([]byte, 8)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err) // TODO: err handling
		}
		return (binary.LittleEndian.Uint64(data))
	case 0xB8:
		log.Print("Handling f16!")
		data := make([]byte, 2)
		_, err := mr.inp.Read(data)
		if err != nil {
			panic(err)
		}
		f16 := float16.Frombits(binary.LittleEndian.Uint16(data))
		// log.Printf("float 16:%s", f16.String())
		return f16
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
		// TODO: string
		return sleb128read(mr.inp).String()
	default:
		panic("Unknown typed value")
	}
}

func (mr *muReader) readTypedArray() any {
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
	switch t {
	case 0xBB:
		for {
			n := uleb128read(mr.inp)
			if n == 0 {
				break
			}

			for i := 0; i < n; i++ {
				val := sleb128read(mr.inp).String()
				res = append(res, val)
			}
			if !chunked {
				return res
			}
		}
	case 0xB8:
		// TODO: support f16
		log.Print("Handling f16!")
		for {
			n := uleb128read(mr.inp)
			if n == 0 {
				break
			}

			for i := 0; i < n; i++ {
				data := make([]byte, 2)
				_, err := mr.inp.Read(data)
				if err != nil {
					panic(err)
				}

				f16 := float16.Frombits(binary.LittleEndian.Uint16(data))
				// fmt.Printf("%v\n", f16)
				res = append(res, f16)
			}
			if !chunked {
				return res
			}
		}
	default:
		for {
			n := uleb128read(mr.inp)
			if n == 0 {
				break
			}

			bits := make([]byte, n*getTypeWidth(t))
			_, err := mr.inp.Read(bits)
			if err != nil {
				panic(err) // TODO: err handling
			}
			// fmt.Printf("%x\n", bits)
			// TODO: handle big endian
			chunk := readArrayFromBits(t, bits)
			if !chunked {
				return chunk
			}

			sliced := chunk.([]any)
			for _, v := range sliced {
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
		res = append(res, mr.ReadObject())
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

func (mr *muReader) readDict() map[string]any {
	res := make(map[string]any)
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
		key, ok := mr.ReadObject().(string)
		if !ok {
			panic(err)
		}
		val := mr.ReadObject()
		res[key] = val
		// log.Printf("key: %s  val: %v", key, val)
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

func (mr *muReader) ReadObject() any {
	data, err := mr.inp.Peek(1)
	var nxt byte
	if err == nil {
		nxt = data[0]
	} else if err == io.EOF {
		fmt.Printf("NO PEEK.%s\n", err)
	} else if err != nil {
		panic(err) // TODO: err handling
	}

	for nxt == 0xFF {
		if _, err := mr.inp.ReadByte(); err != nil {
			panic(err)
		}
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
			return mr.ReadObject()
		case nxt == 0x8B:
			_, err := mr.inp.ReadByte()
			if err != nil {
				panic(err) // TODO: err handling
			}
			_ = uleb128read(mr.inp)
			return mr.ReadObject()
		case nxt == 0x8C:
			_, err := mr.inp.ReadByte()
			if err != nil {
				panic(err) // TODO: err handling
			}
			if mr.peekByte() == 0x90 {
				mr.lru.Extend(mr.readList())
				// Read next object (LRU list is skipped)
				return mr.ReadObject()
			} else {
				res := mr.readString()
				mr.lru.Append(res)
				return res
			}
		case nxt == 0x8F:
			data := make([]byte, 4)
			_, err := mr.inp.Read(data)
			if err != nil {
				panic(err) // TODO: err handling
			}
			if !bytes.Equal([]byte(MuonMagic), data) {
				panic("not muon magic!")
			}
			return mr.ReadObject()
		case nxt == 0x90:
			return mr.readList()
		case nxt == 0x92:
			return mr.readDict()
		default:
			panic("Unknown object")
		}
	}
	return mr.readString()
}

// func dumps(data)

// func loads(data)
