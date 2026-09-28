package driver

import "context"

// Null drops every page, counting them. It exists for tests and for
// `dyj serve --backend null` smoke runs with no writable destination.
type Null struct {
	// Pages printed so far (atomic via mutex; cheap).
	pages int
}

// Name implements Interface.
func (n *Null) Name() string { return "null" }

// Print decodes the document (so corrupt documents still fail honestly)
// and discards the pages.
func (n *Null) Print(ctx context.Context, job PrintJob, doc Document) error {
	pages, errc := Pages(ctx, doc, DecoderOptions{Media: job.Media, Grayscale: !job.Color})
	count := 0
	for range pages {
		count++
	}
	if err := <-errc; err != nil {
		return err
	}
	n.pages += count
	return nil
}

// Pages reports how many pages were discarded.
func (n *Null) Pages() int { return n.pages }
