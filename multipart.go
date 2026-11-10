package allisall

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
)

// The field delimiters a CSV import's [ImportMapping] may name. The API
// reads a comma when Delimiter is nil.
const (
	ImportDelimiterComma     = ","
	ImportDelimiterSemicolon = ";"
	ImportDelimiterTab       = "\t"
)

// defaultFilename is the file name sent for a file that has none.
const defaultFilename = "upload.csv"

// ImportUpload is a CSV import: how the file's columns map to event
// members, and the file itself. It is sent as a form of two parts, the
// mapping and then the file.
type ImportUpload struct {
	// Mapping is sent as the first part, named mapping: its JSON, typed
	// application/json, with the file name mapping.json.
	Mapping ImportMapping
	// File is sent as the second part, named file, typed text/csv. It is
	// read from its position when the call is made to its end, and read
	// again from that position for each retry, so it is never held in
	// memory. Its position is unspecified after the call. A reader that
	// cannot seek, such as an *os.File opened on a pipe, is refused before
	// anything is sent; copy a one-shot stream to a temporary file or a
	// bytes.Reader first.
	File io.ReadSeeker
	// Filename is the file name the import records. When empty, it is the
	// base of the file's own name, for a File with a Name() string method
	// such as an *os.File, and otherwise upload.csv. A name longer than 255
	// characters, counted after any directory part separated by /, is
	// refused with 400 too_long.
	Filename string
}

// form is a CSV import's body, fixed once per call: every attempt sends
// the same bytes, the file re-read from the same position, under the same
// boundary.
type form struct {
	file io.ReadSeeker
	// start is the file's position when the call was made, and length the
	// bytes from there to its end.
	start, length int64
	// prefix is everything before the file's bytes, and suffix everything
	// after them.
	prefix, suffix []byte
	contentType    string
}

// newForm records the file's position and length and writes the form's
// framing. Each error is the caller's mistake, found before sending.
func newForm(u *ImportUpload) (*form, error) {
	if u.File == nil {
		return nil, fmt.Errorf("allisall: the import's File is nil")
	}
	start, err := u.File.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("allisall: the import's file cannot seek: %w", err)
	}
	end, err := u.File.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("allisall: the import's file cannot seek: %w", err)
	}
	if end < start {
		return nil, fmt.Errorf("allisall: the import's file ends at %d, before its position %d", end, start)
	}
	mapping, err := encodeJSON(u.Mapping, "the import's mapping")
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := writePart(w, "mapping", "mapping.json", "application/json", mapping); err != nil {
		return nil, err
	}
	if err := writePart(w, "file", uploadName(u), "text/csv", nil); err != nil {
		return nil, err
	}
	prefix := bytes.Clone(buf.Bytes())
	buf.Reset()
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("allisall: write the import's form: %w", err)
	}
	return &form{
		file:        u.File,
		start:       start,
		length:      end - start,
		prefix:      prefix,
		suffix:      bytes.Clone(buf.Bytes()),
		contentType: w.FormDataContentType(),
	}, nil
}

// writePart writes one part's headers, then content.
func writePart(w *multipart.Writer, name, filename, contentType string, content []byte) error {
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", multipart.FileContentDisposition(name, filename))
	h.Set("Content-Type", contentType)
	pw, err := w.CreatePart(h)
	if err == nil {
		_, err = pw.Write(content)
	}
	if err != nil {
		return fmt.Errorf("allisall: write the import's form: %w", err)
	}
	return nil
}

// uploadName is the file part's file name.
func uploadName(u *ImportUpload) string {
	if u.Filename != "" {
		return u.Filename
	}
	if named, ok := u.File.(interface{ Name() string }); ok && named.Name() != "" {
		if base := filepath.Base(named.Name()); base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return defaultFilename
}

// open rewinds the file and returns the whole body.
func (f *form) open() (io.Reader, error) {
	if _, err := f.file.Seek(f.start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("allisall: rewind the import's file: %w", err)
	}
	return io.MultiReader(bytes.NewReader(f.prefix), io.LimitReader(f.file, f.length), bytes.NewReader(f.suffix)), nil
}

// attach sets the form as the request's body, with its length, and lets
// net/http read it again on its own.
func (f *form) attach(req *http.Request) error {
	body, err := f.open()
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(body)
	req.ContentLength = int64(len(f.prefix)) + f.length + int64(len(f.suffix))
	req.GetBody = func() (io.ReadCloser, error) {
		body, err := f.open()
		if err != nil {
			return nil, err
		}
		return io.NopCloser(body), nil
	}
	return nil
}
