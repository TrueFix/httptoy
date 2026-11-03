package routes

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/gorilla/mux"
)

func multipartResponse(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	body, err := io.ReadAll(req.Body)
	if err != nil {
		bodyError(w, err)
		return
	}
	metadata, err := json.Marshal(map[string]any{"bytes": len(body), "sha256": checksum(body)})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writer := multipart.NewWriter(w)
	w.Header().Set("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": writer.Boundary()}))
	w.Header().Set("Cache-Control", "no-store")
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": []string{CONTENT_JSON}})
	if err != nil {
		return
	}
	if _, err = part.Write(metadata); err != nil {
		return
	}
	part, err = writer.CreatePart(textproto.MIMEHeader{"Content-Type": []string{CONTENT_BYTES}, "Content-Disposition": []string{`attachment; filename="echo.bin"`}})
	if err != nil {
		return
	}
	if _, err = part.Write(body); err != nil {
		return
	}
	_ = writer.Close()
}

const charsetSample = "café déjà vu\n"

func charsetResponse(w http.ResponseWriter, req *http.Request) {
	charset := strings.ToLower(mux.Vars(req)["charset"])
	var data []byte
	switch charset {
	case "utf-8":
		data = []byte(charsetSample)
	case "iso-8859-1":
		for _, ch := range charsetSample {
			data = append(data, byte(ch))
		}
	case "utf-16", "utf-16le", "utf-16be":
		var order binary.AppendByteOrder = binary.BigEndian
		if charset == "utf-16le" {
			order = binary.LittleEndian
		}
		if charset == "utf-16" {
			data = append(data, 0xfe, 0xff)
		}
		for _, unit := range utf16.Encode([]rune(charsetSample)) {
			data = order.AppendUint16(data, unit)
		}
	default:
		invalidRequest(w, "charset must be utf-8, utf-16, utf-16le, utf-16be, or iso-8859-1")
		return
	}
	httpx.Data(w, http.StatusOK, "text/plain; charset="+charset, data)
}

func requestChecksum(w http.ResponseWriter, req *http.Request) {
	algorithm := strings.ToLower(mux.Vars(req)["algorithm"])
	var digest hash.Hash
	switch algorithm {
	case "sha256", "sha-256":
		algorithm = "sha256"
		digest = sha256.New()
	case "sha512", "sha-512":
		algorithm = "sha512"
		digest = sha512.New()
	default:
		invalidRequest(w, "algorithm must be sha256 or sha512")
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	size, err := io.Copy(digest, req.Body)
	if err != nil {
		bodyError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"algorithm": algorithm, "bytes": size, "checksum": hex.EncodeToString(digest.Sum(nil))})
}

func download(w http.ResponseWriter, req *http.Request) {
	filename := mux.Vars(req)["filename"]
	if filename == "" || filename == "." || filename == ".." || len(filename) > 255 || !utf8.ValidString(filename) || strings.ContainsAny(filename, "/\\") {
		invalidRequest(w, "filename must be a valid basename of at most 255 UTF-8 bytes")
		return
	}
	for _, ch := range filename {
		if ch < 32 || ch == 127 {
			invalidRequest(w, "filename cannot contain control characters")
			return
		}
	}
	size, err := boundedNumber(req.URL.Query().Get("size"), 1024, 0, maxBodyBytes)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	disposition := defaultQuery(req, "disposition", "attachment")
	if disposition != "attachment" && disposition != "inline" {
		invalidRequest(w, "disposition must be attachment or inline")
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	httpx.Data(w, http.StatusOK, CONTENT_BYTES, patternBytes(size))
}

func patternBytes(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte('a' + i%26)
	}
	return data
}
