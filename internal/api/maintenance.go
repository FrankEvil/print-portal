package api

import (
	"net/http"
)

// handlePrinterMaintenance returns driver-declared maintenance actions and
// executes one of them.
func (s *Server) handlePrinterMaintenance(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := s.store.GetPrinterByName(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "not_found", "printer not found")
		return
	}
	action := r.PathValue("action")
	if action == "" {
		caps := s.drivers.MaintenanceCapabilities(r.Context(), p.Model)
		if caps.Status == "parsing" {
			s.parseCapabilitiesAsync(p.Model)
		}
		writeJSON(w, http.StatusOK, caps)
		return
	}
	output, err := s.drivers.RunMaintenance(r.Context(), p.Model, action)
	if err != nil {
		writeError(w, http.StatusBadRequest, "maintenance_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "output": output})
}
