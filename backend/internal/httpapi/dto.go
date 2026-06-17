package httpapi

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type registerStartReq struct {
	Email                     string `json:"email"`
	OpaqueRegistrationRequest string `json:"opaque_registration_request"`
}
type registerStartResp struct {
	OpaqueRegistrationResponse string `json:"opaque_registration_response"`
}
type registerFinishReq struct {
	Email                    string `json:"email"`
	OpaqueRegistrationRecord string `json:"opaque_registration_record"`
}
type loginStartReq struct {
	Email string `json:"email"`
	KE1   string `json:"ke1"`
}
type loginStartResp struct {
	LoginID string `json:"login_id"`
	KE2     string `json:"ke2"`
}
type loginFinishReq struct {
	LoginID string `json:"login_id"`
	KE3     string `json:"ke3"`
}
type loginFinishResp struct {
	SessionToken         string `json:"session_token"`
	DeviceEnrollRequired bool   `json:"device_enroll_required"`
}
type sessionResp struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
}

type enrollDeviceReq struct {
	SigningPublicKey   string   `json:"signing_public_key"`
	Label              string   `json:"label"`
	InitialKeyPackages []string `json:"initial_key_packages"`
}
type enrollDeviceResp struct {
	DeviceID string `json:"device_id"`
}
type deviceItem struct {
	DeviceID string `json:"device_id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
}
type keyPackageResp struct {
	KeyPackage   string `json:"key_package"`
	IsLastResort bool   `json:"is_last_resort"`
}
type countResp struct {
	Available int `json:"available"`
}
type uploadKeyPackagesReq struct {
	KeyPackages []string `json:"key_packages"`
}
type addMemberReq struct {
	DeviceID string `json:"device_id"`
	JoinSeq  int64  `json:"join_seq"`
}
