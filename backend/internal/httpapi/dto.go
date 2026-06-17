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
	Username                 string `json:"username"`
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

type sthResp struct {
	TreeSize  int64  `json:"tree_size"`
	RootHash  string `json:"root_hash"`
	Signature string `json:"signature"`
}
type ktKeyResp struct {
	LeafIndex int64    `json:"leaf_index"`
	Version   int64    `json:"version"`
	DeviceSet string   `json:"device_set"`
	AuditPath []string `json:"audit_path"`
	STH       sthResp  `json:"sth"`
}

type createWorkspaceReq struct {
	Name string `json:"name"`
}
type workspaceResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}
type addWSMemberReq struct {
	EmailOrUsername string `json:"email_or_username"`
}
type setWSRoleReq struct {
	Role string `json:"role"`
}
type wsMemberResp struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

type createConvReq struct {
	Type            string `json:"type"`
	Visibility      string `json:"visibility"`
	Name            string `json:"name"`
	EmailOrUsername string `json:"email_or_username"`
}
type convResp struct {
	GroupID    string `json:"group_id"`
	Type       string `json:"type"`
	Visibility string `json:"visibility"`
	Name       string `json:"name"`
}
type addConvUserReq struct {
	EmailOrUsername string `json:"email_or_username"`
}
