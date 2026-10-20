package e2e

import (
	"bytes"
	"io"

	"github.com/232252/dyj/pkg/render"
)

type page = render.Page

func renderTextPages(text string) ([]*render.Page, error) {
	return render.TextPages(text, render.TextOptions{DPI: 300})
}

func newPWGWriter(w io.Writer) (*render.Writer, error) { return render.NewWriter(w) }

var _ = bytes.MinRead
