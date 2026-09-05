package httpx

import (
	"encoding/json"
	"runtime"
)

const stackBufferSize = 8 << 10

func stack() []byte {
	buf := make([]byte, stackBufferSize)
	return buf[:runtime.Stack(buf, false)]
}

func marshalErrorEnvelope(code, message string) ([]byte, error) {
	return json.Marshal(NewErrorEnvelope(code, message))
}
