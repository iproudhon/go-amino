package amino

import (
	_ "encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sync"
	"unsafe"
)

type norefEncoder func(*NoRefCodec, *norefType, unsafe.Pointer, io.Writer) error
type norefDecoder func(*NoRefCodec, *norefType, io.Reader, interface{}) error

type norefType struct {
	aminoTag     string
	jsonTag      string
	tipe         reflect.Type
	kind         reflect.Kind
	elemCount    int
	elemSize     uintptr
	elemType     *norefType
	fieldNames   []string
	fieldTypes   []*norefType
	fieldOffsets []uintptr
	fieldTags    []reflect.StructTag
	encode       norefEncoder
	decode       norefDecoder
}

type NoRefCodec struct {
	mtx        sync.RWMutex
	sealed     bool
	typeInfos  map[reflect.Type]*norefType
	name2types map[string]*norefType
}

// primitive types sans array, chan, func, slice, map & struct
var norefPrimitiveTypes []*norefType

func init() {
	values := []interface{}{
		nil, false, int(0), int8(0), int16(0),
		int32(0), int64(0), uint(0), uint8(0), uint16(0),
		uint32(0), uint64(0), uintptr(0), float32(0), float64(0),
		complex64(0), complex128(0), nil, nil, nil,
		nil, nil, nil, nil, "",
		nil, unsafe.Pointer(uintptr(0)),
	}
	norefPrimitiveTypes = make([]*norefType, len(values))

	for i := 0; i < len(values); i++ {
		if values[i] == nil {
			continue
		}
		tipe := reflect.TypeOf(values[i])
		norefPrimitiveTypes[i] = &norefType{
			tipe:   tipe,
			kind:   tipe.Kind(),
			encode: norefEncodePrimitive,
			decode: norefDecodePrimitive,
		}
	}
}

func norefGetPrimitiveType(kind reflect.Kind) *norefType {
	if kind <= reflect.Invalid || int(kind) >= cap(norefPrimitiveTypes) {
		return nil
	}
	return norefPrimitiveTypes[kind]
}

func norefGenPtrType(tipe *norefType) *norefType {
	return &norefType{
		tipe:         reflect.PtrTo(tipe.tipe),
		kind:         reflect.Ptr,
		elemType:     tipe,
		encode:       norefEncodePtr,
		decode:       norefDecodePtr,
	}
}

func norefGenSliceType(tipe *norefType) *norefType {
	return &norefType{
		tipe:         reflect.SliceOf(tipe.tipe),
		kind:         reflect.Slice,
		elemCount:    0,
		elemSize:     tipe.tipe.Size(),
		elemType:     tipe,
		encode:       norefEncodeSlice,
		decode:       norefDecodeSlice,
	}
}

func norefGenArrayType(count int, tipe *norefType) *norefType {
	return &norefType{
		tipe:         reflect.ArrayOf(count, tipe.tipe),
		kind:         reflect.Array,
		elemCount:    count,
		elemSize:     tipe.tipe.Size(),
		elemType:     tipe,
		encode:       norefEncodeArray,
		decode:       norefDecodeArray,
	}
}

func norefGenMapType(key *norefType, value *norefType) *norefType {
	return &norefType{
		tipe:         reflect.MapOf(key.tipe, value.tipe),
		kind:         reflect.Map,
		fieldNames:   []string{"", ""},
		fieldTypes:   []*norefType{key, value},
		fieldOffsets: []uintptr{0, 0},
		encode:       norefEncodeMap,
		decode:       norefDecodeMap,
	}
}

func NewNoRefCodec() *NoRefCodec {
	cdc := &NoRefCodec{
		sealed:     false,
		typeInfos:  map[reflect.Type]*norefType{},
		name2types: map[string]*norefType{},
	}
	return cdc
}

func (cdc *NoRefCodec) Encode(tipe *norefType, obj interface{}) ([]byte, error) {
	if tipe == nil {
		rt := reflect.TypeOf(obj)
		fmt.Println("RT", rt)
		if t, ok := cdc.typeInfos[rt]; ok {
			tipe = t
		} else if t := norefGetPrimitiveType(rt.Kind()); t != nil {
			tipe = t
		} else {
			return nil, fmt.Errorf("not found: %s", rt)
		}
	}

	y := obj.(string)
	z := unsafe.Pointer(uintptr(unsafe.Pointer(&obj)) + uintptr(0x10))
//	ss := *(*string)(unsafe.Pointer(uintptr(unsafe.Pointer(&obj)) + 0x10))
	ss := *(*string)(z)
	fmt.Println("SS:", ss, unsafe.Pointer(&obj), unsafe.Pointer(&y), z)

	fmt.Println("@@@@", unsafe.Pointer(&obj), unsafe.Pointer(*(*uintptr)(unsafe.Pointer(&obj))))
	
	h := *(*reflect.StringHeader)(z)
	fmt.Println("TYPE", tipe, h)

	
//	err := tipe.encode(cdc, tipe, unsafe.Pointer(&obj), os.Stdout)
	err := tipe.encode(cdc, tipe, z, os.Stdout)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (cdc *NoRefCodec) Decode(tipe *norefType, data []byte, out interface{}) error {
	return nil
}

func (cdc *NoRefCodec) register(rt reflect.Type, name string) (*norefType, error) {
	if _, ok := cdc.typeInfos[rt]; ok {
		return cdc.typeInfos[rt], fmt.Errorf("already exists")
	}
	tipe := norefGetPrimitiveType(rt.Kind())
	if tipe != nil {
		return tipe, nil
	}

	switch rt.Kind() {
	case reflect.Ptr:
		et, err := cdc.register(rt.Elem(), "")
		if err != nil {
			return et, err
		}
		return norefGenPtrType(et), nil
	case reflect.Array:
		et, err := cdc.register(rt.Elem(), "")
		if err != nil {
			return et, err
		}
		return norefGenArrayType(rt.Len(), et), nil
	case reflect.Slice:
		et, err := cdc.register(rt.Elem(), "")
		if err != nil {
			return et, err
		}
		return norefGenSliceType(et), nil
	case reflect.Interface:
		tipe, ok := cdc.typeInfos[rt]
		if ok {
			if tipe.aminoTag != name {
				panic(fmt.Sprintf("interface is registered with different name: %s <= %s != %s",
					rt, tipe.aminoTag, name))
			}
			return tipe, nil
		}
		tipe = &norefType{
			aminoTag: name,
			tipe:     rt,
			kind:     reflect.Interface,
			encode:   norefEncodeInterface,
			decode:   norefDecodeInterface,
		}
		cdc.typeInfos[rt] = tipe
		if len(name) > 0 {
			if _, ok := cdc.name2types[name]; ok {
				panic(fmt.Sprintf("interface %s is already registered as %s",
					rt, name))
			}
			cdc.name2types[name] = tipe
		}
		return tipe, nil
	case reflect.Map:
		kt, err := cdc.register(rt.Key(), "")
		if err != nil {
			return nil, err
		}
		et, err := cdc.register(rt.Elem(), "")
		if err != nil {
			return nil, err
		}
		return norefGenMapType(kt, et), nil
	case reflect.Struct:
		tipe, ok := cdc.typeInfos[rt]
		if ok {
			if tipe.aminoTag != name {
				panic(fmt.Sprintf("type is registered with different name: %s <= %s != %s",
					rt, tipe.aminoTag, name))
			}
			return tipe, nil
		}
		tipe = &norefType{
			aminoTag:     name,
			tipe:         rt,
			kind:         rt.Kind(),
			fieldNames:   []string{},
			fieldTypes:   []*norefType{},
			fieldOffsets: []uintptr{},
			fieldTags:    []reflect.StructTag{},
			encode:       norefEncodeStruct,
			decode:       norefDecodeStruct,
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			ft, err := cdc.register(f.Type, f.Tag.Get("amino"))
			if err != nil {
				return nil, err
			}
			tipe.fieldNames = append(tipe.fieldNames, f.Name)
			tipe.fieldTypes = append(tipe.fieldTypes, ft)
			tipe.fieldOffsets = append(tipe.fieldOffsets, f.Offset)
			tipe.fieldTags = append(tipe.fieldTags, f.Tag)
		}
		cdc.typeInfos[rt] = tipe
		if len(name) > 0 {
			if _, ok := cdc.name2types[name]; ok {
				panic(fmt.Sprintf("interface %s is already registered as %s",
					rt, name))
			}
			cdc.name2types[name] = tipe
		}
		return tipe, nil
	default:
		panic(fmt.Sprint("invalid type: %s", rt.Kind()))
	}

	return nil, fmt.Errorf("not found")
}

func (cdc *NoRefCodec) Register(o interface{}, name string) error {
	var rt reflect.Type
	switch o.(type) {
	case reflect.Type:
		rt = o.(reflect.Type)
	default:
		rt = reflect.TypeOf(o)
	}
	for rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	_, err := cdc.register(rt, name)
	return err
}

func norefEncodePrimitive(cdc *NoRefCodec, tipe *norefType, ptr unsafe.Pointer, w io.Writer) error {
	var o interface{}
	switch tipe.kind {
	case reflect.Bool:
		o = *(*bool)(ptr)
	case reflect.Int:
		o = *(*int)(ptr)
	case reflect.Int8:
		o = *(*int8)(ptr)
	case reflect.Int16:
		o = *(*int)(ptr)
	case reflect.Int32:
		o = *(*int32)(ptr)
	case reflect.Int64:
		o = *(*int64)(ptr)
	case reflect.Uint:
		o = *(*uint)(ptr)
	case reflect.Uint8:
		o = *(*uint8)(ptr)
	case reflect.Uint16:
		o = *(*uint16)(ptr)
	case reflect.Uint32:
		o = *(*uint32)(ptr)
	case reflect.Uint64:
		o = *(*uint64)(ptr)
	case reflect.Uintptr:
		o = *(*uintptr)(ptr)
	case reflect.Float32:
		o = *(*float32)(ptr)
	case reflect.Float64:
		o = *(*float64)(ptr)
	case reflect.Complex64:
		o = *(*complex64)(ptr)
	case reflect.Complex128:
		o = *(*complex128)(ptr)
	case reflect.String:
		h := (*reflect.StringHeader)(ptr)
		fmt.Println("XXX", h)

		
		o = *(*string)(ptr)
	case reflect.UnsafePointer:
		o = ptr
	default:
		panic(fmt.Sprintf("invalid primitive type: %s", tipe.kind))
	}
	w.Write([]byte(fmt.Sprintf("<%s:%v>", tipe.tipe, o)))
	return nil
}

func norefDecodePrimitive(cdc *NoRefCodec, tipe *norefType, r io.Reader, out interface{}) error {
	return nil
}

func norefEncodePtr(cdc *NoRefCodec, tipe *norefType, ptr unsafe.Pointer, w io.Writer) error {
	elem := unsafe.Pointer(*(*uintptr)(ptr))
	return tipe.elemType.encode(cdc, tipe.elemType, elem, w)
}

func norefDecodePtr(cdc *NoRefCodec, tipe *norefType, r io.Reader, out interface{}) error {
	return nil
}

func norefEncodeInterface(cdc *NoRefCodec, tipe *norefType, ptr unsafe.Pointer, w io.Writer) error {
	v := reflect.NewAt(tipe.tipe, ptr).Elem().Elem()
	if v.IsZero() {
		return nil
	}

	elemType, ok := cdc.typeInfos[v.Type()]
	if !ok {
		return fmt.Errorf("not found: %s", v.Type())
	}

	return elemType.encode(cdc, elemType, unsafe.Pointer(v.UnsafeAddr()), w)
}

func norefDecodeInterface(cdc *NoRefCodec, tipe *norefType, r io.Reader, out interface{}) error {
	return nil
}

func norefEncodeArray(cdc *NoRefCodec, tipe *norefType, ptr unsafe.Pointer, w io.Writer) error {
	l := (*reflect.SliceHeader)(ptr)
	w.Write([]byte(fmt.Sprintf("[:%d/%d]%s=>", l.Len, tipe.elemCount, tipe.elemType.tipe)))

	for i := 0; i < l.Len; i++ {
		e := unsafe.Pointer(l.Data + tipe.elemSize * uintptr(i))
		err := tipe.elemType.encode(cdc, tipe.elemType, e, w)
		if err != nil {
			return err
		}
	}
	return nil
}

func norefDecodeArray(cdc *NoRefCodec, tipe *norefType, r io.Reader, out interface{}) error {
	return nil
}

func norefEncodeSlice(cdc *NoRefCodec, tipe *norefType, ptr unsafe.Pointer, w io.Writer) error {
	l := (*reflect.SliceHeader)(ptr)
	w.Write([]byte(fmt.Sprintf("[:%d,%v]%s=>", l.Len, l, tipe.elemType.tipe)))

	for i := 0; i < l.Len && i < 5; i++ {
		e := unsafe.Pointer(l.Data + tipe.elemSize * uintptr(i))
		err := tipe.elemType.encode(cdc, tipe.elemType, e, w)
		if err != nil {
			return err
		}
	}
	return nil
}

func norefDecodeSlice(cdc *NoRefCodec, tipe *norefType, r io.Reader, out interface{}) error {
	return nil
}

func norefEncodeMap(cdc *NoRefCodec, tipe *norefType, ptr unsafe.Pointer, w io.Writer) error {
	w.Write([]byte(fmt.Sprintf("<map:%s>{", tipe.tipe)))

	m := reflect.NewAt(tipe.tipe, ptr).Elem()
	it := m.MapRange()
	for it.Next() {
		k, v := it.Key(), it.Value()
		kptr, vptr := unsafe.Pointer(k.UnsafeAddr()), unsafe.Pointer(v.UnsafeAddr())
		if err := tipe.fieldTypes[0].encode(cdc, tipe.fieldTypes[0], kptr, w); err != nil {
			return err
		}
		w.Write([]byte(":"))
		if err := tipe.fieldTypes[1].encode(cdc, tipe.fieldTypes[1], vptr, w); err != nil {
			return err
		}
		w.Write([]byte(","))
	}
	w.Write([]byte("}"))
	return nil
}

func norefDecodeMap(cdc *NoRefCodec, tipe *norefType, r io.Reader, out interface{}) error {
	return nil
}

func norefEncodeStruct(cdc *NoRefCodec, tipe *norefType, ptr unsafe.Pointer, w io.Writer) error {
	w.Write([]byte(fmt.Sprintf("<%s:%s>", tipe.tipe.Kind(), tipe.tipe)))

	for i := 0; i < len(tipe.fieldNames); i++ {
		w.Write([]byte(fmt.Sprintf("{%s/%d=>", tipe.fieldNames[i], tipe.fieldOffsets[i])))
		ft := tipe.fieldTypes[i];
		fe := unsafe.Pointer(uintptr(ptr) + tipe.fieldOffsets[i])
		if err := ft.encode(cdc, ft, fe, w); err != nil {
			return err
		}
		w.Write([]byte("}"))
	}
	return nil
}

func norefDecodeStruct(cdc *NoRefCodec, tipe *norefType, r io.Reader, out interface{}) error {
	return nil
}

// EOF
