// Generator przykladowego pluginu WASM (wasm-demo).
// Uruchom: go run ./examples/wasm-demo-gen
// Buduje examples/plugins/wasm-demo/main.wasm (recznie zlokalizowany modul WASM,
// bo na maszynie deweloperskiej nie ma TinyGo) i robi smoke-test na wazero.
//
// Gosc eksportuje: memory, alloc (bump allocator), launcher_init (log + storage_set),
// launcher_call (rozpoznaje "ping" po nazwie), cmd_ping, cmd_info.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

var modMem api.Memory

func uleb(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func sleb(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			out = append(out, b)
			return out
		}
		out = append(out, b|0x80)
	}
}

func section(id byte, payload []byte) []byte {
	out := append([]byte{id}, uleb(uint64(len(payload)))...)
	return append(out, payload...)
}

func i32const(v int64) []byte { return append([]byte{0x41}, sleb(v)...) }
func i64const(v int64) []byte { return append([]byte{0x42}, sleb(v)...) }

const (
	heapStart = 4096
	pPong     = 2048
	pInfo     = 2176
	pLog      = 2304
	pKey      = 2432
	pVal      = 2496
)

func packResult(ptr, size int64) int64 { return ptr<<32 | size }

func main() {
	pong := []byte(`{"pong":true}`)
	info := []byte(`{"plugin":"wasm-demo"}`)
	logMsg := []byte("wasm-demo: started")

	// type section: t0 ()->i64; t1 (i32)->i32; t2 (i32*4)->i64; t3 (i32,i32)->(); t4 ()->(); t5 (i32*4)->i32
	types := append([]byte{0x06},
		0x60, 0x00, 0x01, 0x7E,
		0x60, 0x01, 0x7F, 0x01, 0x7F,
		0x60, 0x04, 0x7F, 0x7F, 0x7F, 0x7F, 0x01, 0x7E,
		0x60, 0x02, 0x7F, 0x7F, 0x00,
		0x60, 0x00, 0x00,
		0x60, 0x04, 0x7F, 0x7F, 0x7F, 0x7F, 0x01, 0x7F,
	)

	// import section: launcher.launcher_log (t3, func idx 0), launcher.launcher_storage_set (t5, func idx 1)
	buildImport := func(name string, typeIdx byte) []byte {
		out := uleb(8)
		out = append(out, "launcher"...)
		out = append(out, uleb(uint64(len(name)))...)
		out = append(out, name...)
		out = append(out, 0x00, typeIdx)
		return out
	}
	imports := append(uleb(2), buildImport("launcher_log", 3)...)
	imports = append(imports, buildImport("launcher_storage_set", 5)...)

	// func section: cmd_ping t0, cmd_info t0, alloc t1, launcher_call t2, launcher_init t4
	funcs := append(uleb(5), 0x00, 0x00, 0x01, 0x02, 0x04)

	// global section: (mut i32) = heapStart (bump pointer)
	globals := append(uleb(1), 0x7F, 0x01)
	globals = append(globals, i32const(heapStart)...)
	globals = append(globals, 0x0B)

	// export section (func indices: importy 0-1, zdefiniowane 2-6)
	exports := uleb(6)
	exports = append(exports, uleb(6)...)
	exports = append(exports, []byte("memory")...)
	exports = append(exports, 0x02, 0x00)
	for _, e := range []struct {
		name string
		fn   byte
	}{{"alloc", 4}, {"launcher_call", 5}, {"cmd_ping", 2}, {"cmd_info", 3}, {"launcher_init", 6}} {
		exports = append(exports, uleb(uint64(len(e.name)))...)
		exports = append(exports, e.name...)
		exports = append(exports, 0x00, e.fn)
	}

	// code section
	bodies := [][]byte{
		// cmd_ping: i64.const pack(pPong, len(pong))
		append(i64const(packResult(pPong, int64(len(pong)))), 0x0B),
		// cmd_info: i64.const pack(pInfo, len(info))
		append(i64const(packResult(pInfo, int64(len(info)))), 0x0B),
		// alloc: old = heap; heap += size; return old
		{
			0x23, 0x00, // global.get 0
			0x23, 0x00, // global.get 0
			0x20, 0x00, // local.get 0 (size)
			0x6A,       // i32.add
			0x24, 0x00, // global.set 0
			0x0B,
		},
		// launcher_call: if cmd == "ping" -> pong, else info
		func() []byte {
			out := []byte{}
			cmp := func(off int64, want byte) {
				out = append(out, 0x20, 0x00)               // local.get 0 (cmd_ptr)
				out = append(out, i32const(off)...)         // offset
				out = append(out, 0x6A)                     // i32.add
				out = append(out, 0x2D, 0x00, 0x00)         // i32.load8_u
				out = append(out, i32const(int64(want))...) // want
				out = append(out, 0x73)                     // i32.xor
				if off != 0 {
					out = append(out, 0x72) // i32.or (akumulator)
				}
			}
			cmp(0, 'p')
			cmp(1, 'i')
			cmp(2, 'n')
			cmp(3, 'g')
			out = append(out, 0x04, 0x7E) // if (result i64)
			out = append(out, i64const(packResult(pPong, int64(len(pong))))...)
			out = append(out, 0x05) // else
			out = append(out, i64const(packResult(pInfo, int64(len(info))))...)
			out = append(out, 0x0B, 0x0B) // end if, end func
			return out
		}(),
		// launcher_init: storage_set("boot","1"); log("wasm-demo: started")
		func() []byte {
			out := []byte{}
			out = append(out, i32const(pKey)...)
			out = append(out, i32const(4)...) // len("boot")
			out = append(out, i32const(pVal)...)
			out = append(out, i32const(1)...) // len("1")
			out = append(out, 0x10, 0x01)     // call launcher_storage_set
			out = append(out, 0x1A)           // drop
			out = append(out, i32const(pLog)...)
			out = append(out, i32const(int64(len(logMsg)))...)
			out = append(out, 0x10, 0x00) // call launcher_log
			out = append(out, 0x0B)
			return out
		}(),
	}
	code := uleb(5)
	for _, b := range bodies {
		code = append(code, uleb(uint64(len(b)+1))...)
		code = append(code, 0x00) // locals: none
		code = append(code, b...)
	}
	// data section
	seg := func(addr int64, data []byte) []byte {
		out := []byte{0x00}
		out = append(out, i32const(addr)...)
		out = append(out, 0x0B, byte(len(data)))
		return append(out, data...)
	}
	data := uleb(5)
	data = append(data, seg(pPong, pong)...)
	data = append(data, seg(pInfo, info)...)
	data = append(data, seg(pLog, logMsg)...)
	data = append(data, seg(pKey, []byte("boot"))...)
	data = append(data, seg(pVal, []byte("1"))...)

	bin := append([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00},
		section(1, types)...)
	bin = append(bin, section(2, imports)...)
	bin = append(bin, section(3, funcs)...)
	bin = append(bin, section(5, append(uleb(1), 0x00, 0x01))...) // memory: 1 page
	bin = append(bin, section(6, globals)...)
	bin = append(bin, section(7, exports)...)
	bin = append(bin, section(10, code)...)
	bin = append(bin, section(11, data)...)

	outPath := filepath.Join("examples", "plugins", "wasm-demo", "main.wasm")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(outPath, bin, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("written %s (%d bytes)\n", outPath, len(bin))

	smokeTest(bin, pong)
}

// smokeTest: instantiate modul ze stubem hosta i wywolaj cmd_ping / launcher_call,
// zeby upewnic sie ze wygenerowana binarka jest poprawna zanim trafi do examples.
func smokeTest(bin []byte, pong []byte) {
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer r.Close(context.Background())

	_, err := r.NewHostModuleBuilder("launcher").
		NewFunctionBuilder().WithFunc(func(ptr, size uint32) {
		if modMem != nil {
			if b, ok := modMem.Read(ptr, size); ok {
				fmt.Println("guest log:", string(b))
			}
		}
	}).Export("launcher_log").
		NewFunctionBuilder().WithFunc(func(kp, kl, vp, vl uint32) uint32 {
		return 0
	}).Export("launcher_storage_set").
		Instantiate(ctx)
	if err != nil {
		panic(err)
	}

	mod, err := r.Instantiate(ctx, bin)
	if err != nil {
		panic(fmt.Sprintf("instantiate: %v", err))
	}
	modMem = mod.Memory()
	mem := modMem

	init := mod.ExportedFunction("launcher_init")
	if init != nil {
		if _, err := init.Call(ctx); err != nil {
			panic(fmt.Sprintf("launcher_init: %v", err))
		}
	}

	readPacked := func(packed uint64) string {
		if packed == 0 {
			return ""
		}
		b, _ := mem.Read(uint32(packed>>32), uint32(packed&0xFFFFFFFF))
		return string(b)
	}
	res, err := mod.ExportedFunction("cmd_ping").Call(ctx)
	if err != nil || readPacked(res[0]) != string(pong) {
		panic(fmt.Sprintf("cmd_ping = %q err %v", readPacked(res[0]), err))
	}
	res, err = mod.ExportedFunction("launcher_call").Call(ctx, 2048, 4, 0, 0)
	if err != nil || readPacked(res[0]) != string(pong) {
		panic(fmt.Sprintf("launcher_call(ping) = %q err %v", readPacked(res[0]), err))
	}
	res, err = mod.ExportedFunction("launcher_call").Call(ctx, 2176, 7, 0, 0)
	if err != nil {
		panic(err)
	}
	fmt.Println("smoke-test OK: launcher_init logged, cmd_ping + launcher_call ->", readPacked(res[0]))
}
