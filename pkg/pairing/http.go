// http.go exposes the pairing endpoint: POST /pair with
// {"pin","device_id","device_name"} answers the caller's device token and
// the URI paths its queues should use. The endpoint is intentionally
// unauthenticated — proving the PIN IS the authentication — and is mounted
// outside the admin token gate.
package pairing

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/232252/dyj/pkg/ipp"
)

// SharedPrinter is one printer a successful pairing unlocks.
type SharedPrinter struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Handler serves POST /pair.
type Handler struct {
	Store      *Store
	Printers   func() []SharedPrinter // live printer set
	ServerName func() string          // human name of this dyj host
}

type pairRequest struct {
	PIN        string `json:"pin"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
}

type pairPrinter struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	URIPath string `json:"uri_path"`
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writePairJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var req pairRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<12))
	if err == nil {
		err = json.Unmarshal(body, &req)
	}
	if err != nil || req.DeviceID == "" {
		writePairJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_request"})
		return
	}

	token, err := h.Store.Pair(req.PIN, req.DeviceID, req.DeviceName, hostOnly(r.RemoteAddr))
	switch {
	case err == nil:
		printers := make([]pairPrinter, 0)
		if h.Printers != nil {
			for _, p := range h.Printers() {
				printers = append(printers, pairPrinter{
					Key: p.Key, Name: p.Name,
					URIPath: ipp.TokenPath(token, p.Key),
				})
			}
		}
		name := ""
		if h.ServerName != nil {
			name = h.ServerName()
		}
		writePairJSON(w, http.StatusOK, map[string]any{
			"token":       token,
			"server_name": name,
			"printers":    printers,
		})
	case errors.Is(err, ErrNoPin):
		writePairJSON(w, http.StatusBadRequest, map[string]any{"error": "pin_required"})
	case errors.Is(err, ErrBadPin):
		writePairJSON(w, http.StatusUnauthorized, map[string]any{"error": "bad_pin"})
	case errors.Is(err, ErrLocked):
		wait := time.Duration(0)
		var le *LockedError
		if errors.As(err, &le) {
			wait = le.RetryAfter
		}
		writePairJSON(w, http.StatusForbidden, map[string]any{
			"error":           "locked",
			"retry_after_sec": int(wait.Seconds() + 0.999),
		})
	default:
		writePairJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal"})
	}
}

func writePairJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func hostOnly(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
