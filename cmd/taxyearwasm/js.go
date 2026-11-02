//go:build js

package main

import (
	"syscall/js"
)

// main registers globalThis.taxyear and waits. Each function returns
// {bytes} or {error}.
func main() {
	a := newAPI(true)
	call := func(f func(args []js.Value) ([]byte, error)) js.Func {
		return js.FuncOf(func(_ js.Value, args []js.Value) any {
			b, err := f(args)
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			out := js.Global().Get("Uint8Array").New(len(b))
			js.CopyBytesToJS(out, b)
			return map[string]any{"bytes": out}
		})
	}
	js.Global().Set("taxyear", map[string]any{
		"load": call(func([]js.Value) ([]byte, error) { return nil, a.load() }),
		// setWorking(on) starts a new cache, with or without the engine's
		// line-by-line working.
		"setWorking": call(func(args []js.Value) ([]byte, error) {
			a = newAPI(args[0].Bool())
			return nil, nil
		}),
		"parse": call(func(args []js.Value) ([]byte, error) { return a.parse([]byte(args[0].String())) }),
		// format(file) returns the JSON text as UTF-8 bytes.
		"format": call(func(args []js.Value) ([]byte, error) { return a.format(bytesOf(args[0])) }),
		"compute": call(func(args []js.Value) ([]byte, error) {
			scenario := ""
			if len(args) > 1 && args[1].Type() == js.TypeString {
				scenario = args[1].String()
			}
			return a.compute(bytesOf(args[0]), scenario)
		}),
		"applyEdit": call(func(args []js.Value) ([]byte, error) { return a.applyEdit(bytesOf(args[0]), bytesOf(args[1])) }),
	})
	select {}
}

func bytesOf(v js.Value) []byte {
	b := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(b, v)
	return b
}
