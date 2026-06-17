package httpapi

import (
	"context"
	"net/http"

	"github.com/messenger/backend/internal/store"
)

type kmUsers interface {
	FindByEmailOrUsername(ctx context.Context, q string) (*store.User, error)
}
type kmRoles interface {
	RoleOf(ctx context.Context, workspaceID, userID string) (string, error)
}
type kmDevices interface {
	ActiveDevicesWithKeys(ctx context.Context, userID string) ([]store.DeviceKey, error)
}
type kmKeyPackages interface {
	Consume(ctx context.Context, deviceID string) ([]byte, bool, error)
}

type keyMaterialHandlers struct {
	users   kmUsers
	roles   kmRoles
	devices kmDevices
	keyPkgs kmKeyPackages
}

// get returns, for each active device of the target identity, its signing
// public key and one freshly-consumed KeyPackage. Both the caller and the
// target must be members of the workspace; otherwise the resource is 404.
func (h *keyMaterialHandlers) get(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	wsID := r.PathValue("wsId")
	identity := r.PathValue("identity")

	// Caller must be a workspace member.
	if _, err := h.roles.RoleOf(r.Context(), wsID, swt.Session.UserID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "workspace not found")
		return
	}
	target, err := h.users.FindByEmailOrUsername(r.Context(), identity)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if _, err := h.roles.RoleOf(r.Context(), wsID, target.ID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "user not in workspace")
		return
	}
	devs, err := h.devices.ActiveDevicesWithKeys(r.Context(), target.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "device lookup failed")
		return
	}
	out := make([]keyMaterialResp, 0, len(devs))
	for _, d := range devs {
		kp, _, err := h.keyPkgs.Consume(r.Context(), d.DeviceID)
		if err != nil {
			continue // no key package available for this device; skip it
		}
		out = append(out, keyMaterialResp{
			DeviceID:         d.DeviceID,
			SigningPublicKey: b64enc(d.SigningPublicKey),
			KeyPackage:       b64enc(kp),
		})
	}
	if len(out) == 0 {
		writeError(w, http.StatusNotFound, "no_key_material", "no key packages available for target devices")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
