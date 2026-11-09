package client

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/232252/dyj/pkg/attributes"
)

// 黄金字节：本包对规范 Print-Job 请求编出的字节必须与 fixture 逐字节一致。
// fixture 文件是唯一事实源，任何 wire 格式漂移都会让测试变红。
//
// 文件格式：一行小写十六进制（无分隔符），末尾一个换行，解析见 parseHex。

// goldenRequestAttrs 是被编码的规范请求。改动这里就等于改动 wire 格式，
// 必须重新生成 fixture。
//
//	request-id 1
//	operation-attributes:
//	  attributes-charset        = utf-8
//	  attributes-natural-language = en-us
//	  printer-uri               = ipp://192.0.2.1:6631/p/<token>/ipp/print/eps
//	  job-name                  = dyj golden
//	  document-format           = image/jpeg
//	job-attributes:
//	  copies            = 1
//	  media             = iso_a4_210x297mm
//	  print-color-mode  = color
//	end-of-attributes
func goldenRequestAttrs() ([]attributes.Attribute, []attributes.Attribute) {
	opAttrs := []attributes.Attribute{
		attributes.URI("printer-uri", "ipp://192.0.2.1:6631/p/0123456789abcdef0123456789abcdef/ipp/print/eps"),
		attributes.Name("job-name", "dyj golden"),
		attributes.MimeType("document-format", "image/jpeg"),
	}
	jobAttrs := []attributes.Attribute{
		attributes.Integer("copies", 1),
		attributes.Keyword("media", "iso_a4_210x297mm"),
		attributes.Keyword("print-color-mode", "color"),
	}
	return opAttrs, jobAttrs
}

// goldenFrame 编码规范请求的完整帧（含 end-of-attributes，不含文档流）。
func goldenFrame(t *testing.T) []byte {
	t.Helper()
	opAttrs, jobAttrs := goldenRequestAttrs()
	frame, err := encodeRequest(0x0002, opAttrs, jobAttrs)
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}
	return frame
}

// TestPrintJobGoldenBytes 对比 fixture 文件与真实编码器的输出。
// fixture 缺失时写出它并失败一次（Go 常见的 golden-file 约定）：再跑一次
// 就转入比对模式。
func TestPrintJobGoldenBytes(t *testing.T) {
	want := goldenFrame(t)
	path := filepath.Join("testdata", "print_job.hex")

	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			t.Fatalf("mkdir testdata: %v", mkErr)
		}
		content := hex.EncodeToString(want) + "\n"
		if wErr := os.WriteFile(path, []byte(content), 0o644); wErr != nil {
			t.Fatalf("write golden: %v", wErr)
		}
		t.Fatalf("golden created: %s (%d bytes); run the test again to compare", path, len(want))
	}
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	got, err := parseHex(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden bytes drifted\n want: %s\n  got: %s",
			hex.EncodeToString(want), hex.EncodeToString(got))
	}
}

// TestRoundTripUsesTheSameEncoder 守住抽取 encodeRequest 那次重构：真正
// 发出去的请求体必须等于 encodeRequest 的输出加上文档流。字节序、分隔符
// 或属性顺序一旦在 roundTrip 里走偏，这里立刻变红。
func TestRoundTripUsesTheSameEncoder(t *testing.T) {
	doc := []byte("hello dyj")
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body
		w.Header().Set("Content-Type", "application/ipp")
		_, _ = w.Write(ippResponse(0x0000, group{
			tag: attributes.TagJobAttributes,
			attrs: []attributes.Attribute{
				attributes.Integer("job-id", 7),
			},
		}))
	}))
	defer srv.Close()

	c, err := Parse(ippURI(srv.URL, "ipp", "/ipp/print"), "tester")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	opAttrs, jobAttrs := goldenRequestAttrs()
	if _, err := c.roundTrip(0x0002, opAttrs, jobAttrs, bytes.NewReader(doc), false); err != nil {
		t.Fatalf("roundTrip: %v", err)
	}

	frame, err := encodeRequest(0x0002, opAttrs, jobAttrs)
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}
	want := append(append([]byte{}, frame...), doc...)
	if !bytes.Equal(captured, want) {
		t.Errorf("request body drifted from encodeRequest\n want: %s\n  got: %s",
			hex.EncodeToString(want), hex.EncodeToString(captured))
	}
}

// parseHex 解析 fixture 文件的内容：十六进制字节，空白分隔或不分隔都行。
func parseHex(s string) ([]byte, error) {
	cleaned := strings.NewReplacer(" ", "", "\t", "", "\n", "", "\r", "").Replace(s)
	cleaned = strings.TrimPrefix(cleaned, "0x")
	if cleaned == "" {
		return nil, nil
	}
	return hex.DecodeString(cleaned)
}
