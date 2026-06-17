//go:build js && wasm

package main

import (
	"encoding/base64"
	"syscall/js"
)

func b64d(s string) []byte { b, _ := base64.StdEncoding.DecodeString(s); return b }
func b64e(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
func errObj(err error) any { return map[string]any{"error": err.Error()} }

func jsRegInit(_ js.Value, args []js.Value) any {
	flow, req, err := RegInit(b64d(args[0].String()))
	if err != nil {
		return errObj(err)
	}
	return map[string]any{"flowId": flow, "request": b64e(req)}
}
func jsRegFinalize(_ js.Value, args []js.Value) any {
	rec, ek, err := RegFinalize(args[0].String(), b64d(args[1].String()), b64d(args[2].String()))
	if err != nil {
		return errObj(err)
	}
	return map[string]any{"record": b64e(rec), "exportKey": b64e(ek)}
}
func jsLoginKE1(_ js.Value, args []js.Value) any {
	flow, ke1, err := LoginKE1(b64d(args[0].String()))
	if err != nil {
		return errObj(err)
	}
	return map[string]any{"flowId": flow, "ke1": b64e(ke1)}
}
func jsLoginKE3(_ js.Value, args []js.Value) any {
	ke3, sk, err := LoginKE3(args[0].String(), b64d(args[1].String()), b64d(args[2].String()))
	if err != nil {
		return errObj(err)
	}
	return map[string]any{"ke3": b64e(ke3), "sessionKey": b64e(sk)}
}

func main() {
	js.Global().Set("opaqueRegInit", js.FuncOf(jsRegInit))
	js.Global().Set("opaqueRegFinalize", js.FuncOf(jsRegFinalize))
	js.Global().Set("opaqueLoginKE1", js.FuncOf(jsLoginKE1))
	js.Global().Set("opaqueLoginKE3", js.FuncOf(jsLoginKE3))
	select {}
}
