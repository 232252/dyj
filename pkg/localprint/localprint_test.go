package localprint

import "testing"

func TestIsVirtual(t *testing.T) {
	cases := []struct {
		name, driver, port string
		want               bool
	}{
		{"EPSON L3250 Series", "EPSON L3250 Series", "192.168.1.50", false},
		{"Microsoft Print to PDF", "Microsoft Print To PDF", "PORTPROMPT:", true},
		{"Microsoft Print to PDF", "Microsoft Print To PDF", "172.99.1.1", true}, // driver alone
		{"doc writer", "Microsoft XPS Document Writer", "PORTPROMPT:", true},
		{"AnyQueue", "AnyDriver", "nul:", true},                 // port alone
		{"Fax", "Microsoft Shared Fax Driver", "SHRFAX:", true}, // name+port
		{"OneNote (Desktop)", "Send to OneNote 16 Driver", "nul:", true},
		{"OneNote 2020", "Generic Driver", "10.0.0.5", true}, // name hint
		{"HP LaserJet", "HP Universal Printing", "USB001", false},
		{"", "", "", false},
		{"导出为WPS PDF", "Kingsoft Virtual Printer Driver", "Kingsoft Virtual Printer Port", true},
	}
	for _, c := range cases {
		if got := IsVirtual(c.name, c.driver, c.port); got != c.want {
			t.Errorf("IsVirtual(%q,%q,%q) = %v, want %v", c.name, c.driver, c.port, got, c.want)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"EPSON L3250 Series", "epson-l3250-series"},
		{"HP_LaserJet 1020", "hp-laserjet-1020"},
		{"办公室打印机", "printer"}, // CJK folds away entirely
		{"---", "printer"},
		{"  X  ", "x"},
		{"Epson  (2)", "epson-2"},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
