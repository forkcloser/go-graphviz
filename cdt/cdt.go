// Package cdt mirrors libcdt, Graphviz's container data types: the
// dictionaries, their links, methods and disciplines, as typed handles over
// the WebAssembly module's memory.
package cdt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

type Dict struct {
	wasm *wasm.Dict
}

func toDict(v *wasm.Dict) *Dict {
	if v == nil {
		return nil
	}

	return &Dict{wasm: v}
}

func (d *Dict) Close() error {
	res, err := d.wasm.Close(context.Background())
	if err != nil {
		return err
	}

	return toError(res)
}

func (d *Dict) View(dict *Dict) (*Dict, error) {
	res, err := d.wasm.View(context.Background(), dict.getWasm())
	if err != nil {
		return nil, err
	}

	return toDict(res), nil
}

func (d *Dict) Disc(disc *Disc) (*Disc, error) {
	res, err := d.wasm.Disc(context.Background(), disc.getWasm())
	if err != nil {
		return nil, err
	}

	return toDisc(res), nil
}

func (d *Dict) Method(mtd *Method) (*Method, error) {
	res, err := d.wasm.Method(context.Background(), mtd.getWasm())
	if err != nil {
		return nil, err
	}

	return toMethod(res), nil
}

func (d *Dict) Flatten() (*Link, error) {
	res, err := d.wasm.Flatten(context.Background())
	if err != nil {
		return nil, err
	}

	return toLink(res), nil
}

func (d *Dict) Extract() (*Link, error) {
	res, err := d.wasm.Extract(context.Background())
	if err != nil {
		return nil, err
	}

	return toLink(res), nil
}

func (d *Dict) Restore(link *Link) error {
	res, err := d.wasm.Restore(context.Background(), link.getWasm())
	if err != nil {
		return err
	}

	return toError(res)
}

func (d *Dict) Walk(fn func(context.Context, *Dict, any, any) error, data any) error {
	// TODO
	res, err := d.wasm.Walk(
		context.Background(),
		wasm.CreateCallbackFunc(func(ctx context.Context, a1, a2 any) (int, error) {
			if err := fn(ctx, d, a1, a2); err != nil {
				return 0, err
			}

			return 0, nil
		}, wasm.WasmPtr(d.wasm)),
		data,
	)
	if err != nil {
		return err
	}

	return toError(res)
}

func (d *Dict) Renew(a0 any) (any, error) {
	res, err := d.wasm.Renew(context.Background(), a0)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (d *Dict) Size() (int, error) {
	res, err := d.wasm.Size(context.Background())
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (d *Dict) Stat(a0 *Stat, a1 int) (int, error) {
	res, err := d.wasm.Stat(context.Background(), a0.getWasm(), a1)
	if err != nil {
		return 0, err
	}

	return res, nil
}

func (d *Dict) getWasm() *wasm.Dict {
	return d.wasm
}

type Link struct {
	wasm *wasm.DictLink
}

func toLink(v *wasm.DictLink) *Link {
	if v == nil {
		return nil
	}

	return &Link{wasm: v}
}

func (l *Link) Right() *Link {
	return toLink(l.wasm.GetRight())
}

func (l *Link) SetRight(v *Link) {
	l.wasm.SetRight(v.getWasm())
}

func (l *Link) Left() *Link {
	return toLink(l.wasm.GetLeft())
}

func (l *Link) SetLeft(v *Link) {
	l.wasm.SetLeft(v.getWasm())
}

func (l *Link) Hash() uint {
	return uint(l.wasm.GetHash())
}

// SetHash sets the link's hash; the field is 32 bits wide in libcdt, so the
// parameter is too.
func (l *Link) SetHash(v uint32) {
	l.wasm.SetHash(v)
}

func (l *Link) getWasm() *wasm.DictLink {
	return l.wasm
}

type Method struct {
	wasm *wasm.DictMethod
}

func toMethod(v *wasm.DictMethod) *Method {
	if v == nil {
		return nil
	}

	return &Method{wasm: v}
}

func (m *Method) getWasm() *wasm.DictMethod {
	return m.wasm
}

type Disc struct {
	wasm *wasm.DictDisc
}

func toDisc(v *wasm.DictDisc) *Disc {
	if v == nil {
		return nil
	}

	return &Disc{wasm: v}
}

func (d *Disc) getWasm() *wasm.DictDisc {
	return d.wasm
}

type Stat struct {
	wasm *wasm.DictStat
}

func (s *Stat) getWasm() *wasm.DictStat {
	return s.wasm
}

type (
	Search  func(*Dict, any, int) any
	Make    func(*Dict, any, *Disc) any
	Memory  func(*Dict, any, uint, *Disc) any
	Free    func(*Dict, any, *Disc)
	Compare func(*Dict, any, any, *Disc) int
	Hash    func(*Dict, any, *Disc) uint
	Event   func(*Dict, int, any, *Disc) int
)

func StrHash(a1 any, a2 int) (uint, error) {
	return wasm.StrHash(context.Background(), a1, a2)
}

func Open(disc *Disc, mtd *Method) (*Dict, error) {
	res, err := wasm.NewDictWithDisc(context.Background(), disc.getWasm(), mtd.getWasm())
	if err != nil {
		return nil, err
	}

	return toDict(res), nil
}

// ErrDict is the error every failed libcdt call wraps; the message Graphviz
// left in its error buffer follows it.
var ErrDict = errors.New("cdt")

// toError maps a libcdt result code: zero is success, anything else is a
// failure, with the message Graphviz reported when it reported one.
func toError(result int) error {
	if result == 0 {
		return nil
	}

	if text := wasm.TakeLastError(); text != "" {
		return fmt.Errorf("%w: %s", ErrDict, strings.TrimRight(text, "\n"))
	}

	return fmt.Errorf("%w: call failed with code %d and no message", ErrDict, result)
}
