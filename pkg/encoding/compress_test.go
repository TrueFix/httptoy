package encoding

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestCompressRoundTrip(t *testing.T) {
	for _, codec := range []struct {
		name       string
		compress   func([]byte) []byte
		decompress func(io.Reader) (io.ReadCloser, error)
	}{
		{"flate", CompressFlate, func(r io.Reader) (io.ReadCloser, error) { return flate.NewReader(r), nil }},
		{"gzip", CompressGzip, func(r io.Reader) (io.ReadCloser, error) { return gzip.NewReader(r) }},
		{"zlib", CompressZlib, zlib.NewReader},
		{"brotli", CompressBrotli, func(r io.Reader) (io.ReadCloser, error) { return io.NopCloser(brotli.NewReader(r)), nil }},
	} {
		t.Run(codec.name, func(t *testing.T) {
			for _, input := range [][]byte{nil, []byte("i love httptoy"), []byte("i ❤️ httptoy 🎉"), bytes.Repeat([]byte{0, 1, 255}, 1024)} {
				reader, err := codec.decompress(bytes.NewReader(codec.compress(input)))
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(reader)
				closeErr := reader.Close()
				if err != nil {
					t.Fatal(err)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
				if !bytes.Equal(got, input) {
					t.Fatalf("round trip = %q, want %q", got, input)
				}
			}
		})
	}
}
