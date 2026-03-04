package gomailconn

import (
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"simple", "doc.pdf", "doc.pdf"},
		{"removes dotdot", "..", ""},
		{"removes path slash", "a/b/c.pdf", "c.pdf"},
		{"removes backslash", `a\b\c.pdf`, "a_b_c.pdf"},
		{"base only", "file.txt", "file.txt"},
		{"mixed", "../dir/file.txt", "file.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCovertContentTypeToBodyContentType(t *testing.T) {
	tests := []struct {
		in   string
		want BodyContentType
	}{
		{"", ""},
		{"text/html", BodyContentTypeHtml},
		{"text/html; charset=utf-8", BodyContentTypeHtml},
		{"text/plain", BodyContentTypeText},
		{"text/plain; charset=us-ascii", BodyContentTypeText},
		{"text/xml", BodyContentTypeText},
		{"application/pdf", ""},
		{"image/png", ""},
	}
	for _, tt := range tests {
		got := covertContentTypeToBodyContentType(tt.in)
		if got != tt.want {
			t.Errorf("covertContentTypeToBodyContentType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseFilenameFromDisposition(t *testing.T) {
	tests := []struct {
		name string
		disp string
		want string
	}{
		{"empty", "", ""},
		{"no filename", "attachment", ""},
		{"filename unquoted", "attachment; filename=doc.pdf", "doc.pdf"},
		{"filename quoted", `attachment; filename="doc.pdf"`, "doc.pdf"},
		{"inline", "inline; filename=img.png", "img.png"},
		{"with semicolon", "attachment; filename=file.pdf; size=1024", "file.pdf"},
		{"lowercase", "ATTACHMENT; FILENAME=doc.pdf", "doc.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFilenameFromDisposition(tt.disp)
			if got != tt.want {
				t.Errorf("parseFilenameFromDisposition(%q) = %q, want %q", tt.disp, got, tt.want)
			}
		})
	}
}

func TestGetPartFileSizeFromDisposition(t *testing.T) {
	tests := []struct {
		name   string
		disp   string
		want   int64
		wantOk bool
	}{
		{"empty", "", 0, false},
		{"no size", "attachment; filename=a.pdf", 0, false},
		{"with size", "attachment; filename=a.pdf; size=12345", 12345, true},
		{"inline size", "inline; size=999", 999, true},
		{"invalid size", "attachment; size=abc", 0, false},
		{"uppercase", "ATTACHMENT; SIZE=42", 42, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := getPartFileSizeFromDisposition(tt.disp)
			if ok != tt.wantOk || got != tt.want {
				t.Errorf("getPartFileSizeFromDisposition(%q) = %d, %v, want %d, %v", tt.disp, got, ok, tt.want, tt.wantOk)
			}
		})
	}
}

func TestDecodeRFC2047Filename(t *testing.T) {

	tests := []struct {
		name string
		s    string
		want string
	}{
		{"normal", "正常.png", "正常.png"},
		{"GB2312-Quoted-Printable", "=?GB2312?Q?gb2312=B2=E2=CA=D4=B2=E2=CA=D4.png?=", "gb2312测试测试.png"},
		{"no encoding", "file.pdf", "file.pdf"},
		{"invalid encoding", "=?UTF-8?Q?file.pdf?=", "file.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeRFC2047Filename(tt.s); got != tt.want {
				t.Errorf("decodeRFC2047Filename(%q) = %q, want %q", tt.s, got, tt.want)
			}
		})
	}

}
