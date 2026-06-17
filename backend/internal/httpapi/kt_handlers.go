package httpapi

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"

	"github.com/messenger/backend/internal/kt"
	"github.com/messenger/backend/internal/store"
)

type ktHandlers struct {
	svc    *kt.Service
	pubKey ed25519.PublicKey
}

func b64slice(in [][]byte) []string {
	out := make([]string, len(in))
	for i, b := range in {
		out[i] = base64.StdEncoding.EncodeToString(b)
	}
	return out
}

func sthDTO(s *store.KTSTH) sthResp {
	return sthResp{
		TreeSize:  s.TreeSize,
		RootHash:  base64.StdEncoding.EncodeToString(s.RootHash),
		Signature: base64.StdEncoding.EncodeToString(s.Signature),
	}
}

func (h *ktHandlers) pubkey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"kt_public_key": base64.StdEncoding.EncodeToString(h.pubKey),
	})
}

func (h *ktHandlers) sth(w http.ResponseWriter, r *http.Request) {
	sth, err := h.svc.LatestSTH(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, "no_sth", "no STH yet")
		return
	}
	writeJSON(w, http.StatusOK, sthDTO(sth))
}

func (h *ktHandlers) key(w http.ResponseWriter, r *http.Request) {
	identity := r.PathValue("identity")
	res, err := h.svc.Lookup(r.Context(), identity)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no key record for identity")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, ktKeyResp{
		LeafIndex: res.LeafIndex,
		Version:   res.Version,
		DeviceSet: base64.StdEncoding.EncodeToString(res.DeviceSet),
		AuditPath: b64slice(res.AuditPath),
		STH:       sthDTO(&res.STH),
	})
}

func (h *ktHandlers) inclusion(w http.ResponseWriter, r *http.Request) {
	leafIndex, err1 := strconv.ParseInt(r.URL.Query().Get("leaf_index"), 10, 64)
	treeSize, err2 := strconv.ParseInt(r.URL.Query().Get("tree_size"), 10, 64)
	if err1 != nil || err2 != nil || leafIndex < 0 || treeSize <= leafIndex {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid leaf_index/tree_size")
		return
	}
	path, err := h.svc.Inclusion(r.Context(), leafIndex, treeSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, "proof_failed", "could not build inclusion proof")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"leaf_index": leafIndex, "tree_size": treeSize, "audit_path": b64slice(path)})
}

func (h *ktHandlers) consistency(w http.ResponseWriter, r *http.Request) {
	from, err1 := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	to, err2 := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	if err1 != nil || err2 != nil || from < 0 || to < from {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid from/to")
		return
	}
	pf, err := h.svc.Consistency(r.Context(), from, to)
	if err != nil {
		writeError(w, http.StatusBadRequest, "proof_failed", "could not build consistency proof")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "proof": b64slice(pf)})
}
