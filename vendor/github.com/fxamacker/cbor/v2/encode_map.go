// Copyright (c) Faye Amacker. All rights reserved.
// Licensed under the MIT License. See LICENSE in the project root for license information.

package cbor

import (
	"reflect"
	"sync"
)

type mapKeyValueEncodeFunc struct {
	kf, ef       encodeFunc
	kpool, vpool sync.Pool
}

func (me *mapKeyValueEncodeFunc) encodeKeyValues(dst []byte, em *encMode, v reflect.Value, kvs []keyValue) ([]byte, error) {
	iterk := me.kpool.Get().(*reflect.Value)
	defer func() {
		iterk.SetZero()
		me.kpool.Put(iterk)
	}()
	iterv := me.vpool.Get().(*reflect.Value)
	defer func() {
		iterv.SetZero()
		me.vpool.Put(iterv)
	}()

	var err error

	if kvs == nil {
		for i, iter := 0, v.MapRange(); iter.Next(); i++ {
			iterk.SetIterKey(iter)
			iterv.SetIterValue(iter)

			if dst, err = me.kf(dst, em, *iterk); err != nil {
				return dst, err
			}
			if dst, err = me.ef(dst, em, *iterv); err != nil {
				return dst, err
			}
		}
		return dst, nil
	}

	initial := len(dst)
	for i, iter := 0, v.MapRange(); iter.Next(); i++ {
		iterk.SetIterKey(iter)
		iterv.SetIterValue(iter)

		offset := len(dst)
		if dst, err = me.kf(dst, em, *iterk); err != nil {
			return dst, err
		}
		valueOffset := len(dst)
		if dst, err = me.ef(dst, em, *iterv); err != nil {
			return dst, err
		}
		kvs[i] = keyValue{
			offset:      offset - initial,
			valueOffset: valueOffset - initial,
			nextOffset:  len(dst) - initial,
		}
	}

	return dst, nil
}

func getEncodeMapFunc(t reflect.Type, newEncodeFuncs map[reflect.Type]*inProgressEncodeFuncs) encodeFunc {
	kf := getEncodeFuncWithNewEncodeFuncs(t.Key(), newEncodeFuncs)
	ef := getEncodeFuncWithNewEncodeFuncs(t.Elem(), newEncodeFuncs)
	if kf == nil || ef == nil {
		return nil
	}
	mkv := &mapKeyValueEncodeFunc{
		kf: kf,
		ef: ef,
		kpool: sync.Pool{
			New: func() any {
				rk := reflect.New(t.Key()).Elem()
				return &rk
			},
		},
		vpool: sync.Pool{
			New: func() any {
				rv := reflect.New(t.Elem()).Elem()
				return &rv
			},
		},
	}
	return mapEncodeFunc{
		e: mkv.encodeKeyValues,
	}.encode
}
