package storage

import (
	"encoding/json"
	"net/http"

	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
)

type StatusHandler struct{ monitor *Monitor }

func NewStatusHandler(monitor *Monitor) *StatusHandler { return &StatusHandler{monitor: monitor} }

func (h *StatusHandler) GetStorageRuntimeStatus(w http.ResponseWriter, _ *http.Request) {
	snapshot := h.monitor.Snapshot()
	response := httpapi.StorageRuntimeStatus{
		Healthy:       snapshot.Healthy,
		Reason:        httpapi.StorageRuntimeStatusReason(snapshot.Reason),
		LastCheckedAt: snapshot.LastCheckedAt,
		ChangedAt:     snapshot.ChangedAt,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
