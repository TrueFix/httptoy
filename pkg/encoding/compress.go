package encoding

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"

	"github.com/andybalholm/brotli"
)

// CompressFlate returns raw DEFLATE data. For HTTP Content-Encoding: deflate,
// use CompressZlib instead, as HTTP requires the zlib wrapper.
func CompressFlate(data []byte) []byte {
	var b bytes.Buffer
	w, _ := flate.NewWriter(&b, 9)
	w.Write(data)
	w.Close()

	return b.Bytes()
}

func CompressGzip(data []byte) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	gz.Write(data)
	gz.Close()

	return b.Bytes()
}

func CompressZlib(data []byte) []byte {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	w.Write(data)
	w.Close()

	return b.Bytes()
}

func CompressBrotli(data []byte) []byte {
	var b bytes.Buffer
	w := brotli.NewWriterLevel(&b, brotli.BestCompression)
	w.Write(data)
	w.Close()
	return b.Bytes()
}
