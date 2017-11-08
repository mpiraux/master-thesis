package masterthesis

/*This file originates from https://github.com/ekr/minq and is subject to the
following license and copyright

-------------------------------------------------------------------------------

The MIT License (MIT)

Copyright (c) 2016 Eric Rescorla

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.

*/

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

const (
	codecDefaultSize = ^uintptr(0)
)

func uintEncode(buf *bytes.Buffer, v reflect.Value, encodingSize uintptr) error {
	size := v.Type().Size()
	if encodingSize != codecDefaultSize {
		if encodingSize > size {
			return fmt.Errorf("Requested a length longer than the native type")
		}
		size = encodingSize
	}

	val := v.Uint()
	// Now encode the low-order bytes of the value.
	for b := size; b > 0; b -= 1 {
		buf.WriteByte(byte(val >> ((b - 1) * 8)))
	}

	return nil
}

func arrayEncode(buf *bytes.Buffer, v reflect.Value) error {
	b := v.Bytes()
	buf.Write(b)

	return nil
}

// Check to see if fields
func ignoreField(name string) bool {
	return unicode.IsLower(rune(name[0]))
}

// Length specifications are of the form:
//
// lengthbits: "B:L1,L2,...LN
//
// where B is the rightmost bit of the length bits and
// L_n are the various lengths (in bytes) indicated by
// the bit values in sequence. N must be a power of 2
// and the right number of bytes is drawn to compute it.
type lengthSpec struct {
	rightBit uint
	numBits  uint
	values   []int
}

func parseLengthSpecification(spec string) (*lengthSpec, error) {
	spl := strings.Split(spec, ":")

	// Rightmost bit.
	p, err := strconv.ParseUint(spl[0], 10, 8)
	if err != nil {
		return nil, err
	}
	bitr := uint(p)
	vals := strings.Split(spl[1], ",")

	// Figure out how many bits we need.
	nvals := int(1)
	var bits int
	for bits = 1; bits <= 8; bits++ {
		nvals <<= 1
		if nvals == len(vals) {
			break
		}
	}

	// Now compute the values
	valArr := make([]int, nvals)
	for i, v := range vals {
		valArr[i], err = strconv.Atoi(v)
		if err != nil {
			return nil, err
		}
	}

	return &lengthSpec{
		bitr,
		uint(bits),
		valArr,
	}, nil
}

func computeLengthFromSpec(t byte, f reflect.StructField) uintptr {
	st := f.Tag.Get("lengthbits")
	if st == "" {
		return codecDefaultSize
	}

	spec, _ := parseLengthSpecification(st)

	mask := byte(0)
	bit := uint(0)
	for ; bit < spec.numBits; bit++ {
		mask |= (1 << bit)
	}
	idx := int(t >> (spec.rightBit - 1) & mask)

	return uintptr(spec.values[idx])
}

// Encode all the fields of a struct to a bytestring.
func encode(i interface{}) (ret []byte, err error) {
	var buf bytes.Buffer
	var res error
	reflected := reflect.ValueOf(i).Elem()
	fields := reflected.NumField()

	for j := 0; j < fields; j += 1 {
		field := reflected.Field(j)
		tipe := reflected.Type().Field(j)

		if ignoreField(tipe.Name) {
			continue
		}

		switch field.Kind() {
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			// Call the length overrider to tell us if we shoud be using a shorter
			// encoding.
			encodingSize := uintptr(codecDefaultSize)
			lFunc, getLength := reflected.Type().MethodByName(tipe.Name + "__length")
			if getLength {
				length_result := lFunc.Func.Call([]reflect.Value{reflect.ValueOf(i).Elem()})
				encodingSize = uintptr(length_result[0].Uint())
			}
			res = uintEncode(&buf, field, encodingSize)
		case reflect.Array, reflect.Slice:
			res = arrayEncode(&buf, field)
		default:
			return nil, fmt.Errorf("Unknown type")
		}

		if res != nil {
			return nil, res
		}
	}

	ret = buf.Bytes()
	return ret, nil
}

func uintDecodeInt(buf *bytes.Reader, size uintptr) (uint64, error) {
	val := make([]byte, size)
	rv, err := buf.Read(val)
	if err != nil {
		return 0, err
	}
	if rv != int(size) {
		return 0, fmt.Errorf("Not enough bytes in buffer")
	}

	tmp := uint64(0)
	for b := uintptr(0); b < size; b += 1 {
		tmp = (tmp << 8) + uint64(val[b])
	}
	return tmp, nil
}

func uintDecode(buf *bytes.Reader, v reflect.Value, encodingSize uintptr) (uintptr, error) {
	size := v.Type().Size()
	if encodingSize != codecDefaultSize {
		if encodingSize > size {
			return 0, fmt.Errorf("Requested a length longer than the native type")
		}
		size = encodingSize
	}

	tmp, err := uintDecodeInt(buf, size)
	if err != nil {
		return 0, err
	}

	v.SetUint(tmp)

	return size, nil
}

func encodeArgs(args ...interface{}) []byte {
	var buf bytes.Buffer
	var res error

	for _, arg := range args {
		reflected := reflect.ValueOf(arg)
		switch reflected.Kind() {
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			res = uintEncode(&buf, reflected, codecDefaultSize)
		case reflect.Array, reflect.Slice:
			res = arrayEncode(&buf, reflected)
		default:
			panic(fmt.Sprintf("Unknown type"))
		}
		if res != nil {
			panic(fmt.Sprintf("Encoding error"))
		}
	}

	return buf.Bytes()
}

func arrayDecode(buf *bytes.Reader, v reflect.Value, encodingSize uintptr) (uintptr, error) {
	if encodingSize == codecDefaultSize {
		encodingSize = uintptr(buf.Len())
	}

	val := make([]byte, encodingSize)

	// Go will return EOF if you try to read 0 bytes off a closed stream.
	if encodingSize == 0 {
		return 0, nil
	}
	rv, err := buf.Read(val)
	if err != nil {
		return 0, err
	}
	if rv != int(encodingSize) {
		return 0, fmt.Errorf("Not enough bytes in buffer")
	}

	v.SetBytes(val)
	return encodingSize, nil
}

// Decode all the fields of a struct from a bytestring. Takes
// a pointer to the struct to fill in
func decode(i interface{}, data []byte) (uintptr, error) {
	buf := bytes.NewReader(data)
	var res error
	reflected := reflect.ValueOf(i).Elem()
	fields := reflected.NumField()
	bytesread := uintptr(0)

	for j := 0; j < fields; j += 1 {
		br := uintptr(0)
		field := reflected.Field(j)
		tipe := reflected.Type().Field(j)

		if ignoreField(tipe.Name) {
			continue
		}

		// Call the length overrider to tell us if we should be using a shorter
		// encoding.
		encodingSize := uintptr(codecDefaultSize)
		lFunc, getLength := reflected.Type().MethodByName(tipe.Name + "__length")
		if getLength {
			length_result := lFunc.Func.Call([]reflect.Value{reflect.ValueOf(i).Elem()})
			encodingSize = uintptr(length_result[0].Uint())
		}

		switch field.Kind() {
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			br, res = uintDecode(buf, field, encodingSize)
		case reflect.Array, reflect.Slice:
			br, res = arrayDecode(buf, field, encodingSize)
		default:
			return 0, fmt.Errorf("Unknown type")
		}
		if res != nil {
			return bytesread, res
		}
		bytesread += br
	}

	return bytesread, nil
}
